package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/api/handler"
	"domain-platform/api/middleware"
	"domain-platform/api/router"
	agentsvc "domain-platform/internal/agent"
	"domain-platform/internal/auth"
	"domain-platform/internal/bootstrap"
	costsvc "domain-platform/internal/cost"
	domainsvc "domain-platform/internal/domain"
	"domain-platform/internal/dnsprovider"
	"domain-platform/internal/dnsquery"
	dnsrecsvc "domain-platform/internal/dnsrecord"
	importsvc "domain-platform/internal/importer"
	"domain-platform/internal/lifecycle"
	"domain-platform/internal/project"
	"domain-platform/internal/registrar"
	"domain-platform/internal/release"
	sslsvc "domain-platform/internal/ssl"
	tagsvc "domain-platform/internal/tag"
	tmplsvc "domain-platform/internal/template"
	pkgstorage "domain-platform/pkg/storage"
	"domain-platform/store/postgres"
)

func main() {
	cfg, err := bootstrap.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := bootstrap.NewLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	db, err := bootstrap.NewDB(cfg.DB)
	if err != nil {
		logger.Fatal("connect postgres", zap.Error(err))
	}
	defer db.Close()

	rdb, err := bootstrap.NewRedis(cfg.Redis)
	if err != nil {
		logger.Fatal("connect redis", zap.Error(err))
	}
	defer rdb.Close()

	asynqClient := bootstrap.NewAsynqClient(cfg.Redis)
	defer asynqClient.Close()

	storageClient, err := bootstrap.NewStorage(cfg.Storage)
	if err != nil {
		logger.Fatal("connect storage", zap.Error(err))
	}
	// ── Auth + stores ─────────────────────────────────────────────────────
	userStore := postgres.NewUserStore(db)
	roleStore := postgres.NewRoleStore(db)
	jwtMgr := auth.NewJWTManager(cfg.JWT.Secret, cfg.JWT.Expiry)
	authSvc := auth.NewService(userStore, roleStore, jwtMgr, logger)
	authHandler := handler.NewAuthHandler(authSvc, userStore, roleStore, logger)

	projectStore := postgres.NewProjectStore(db)
	projectSvc := project.NewService(projectStore, logger)
	projectHandler := handler.NewProjectHandler(projectSvc, logger)

	domainStore := postgres.NewDomainStore(db)
	lifecycleStore := postgres.NewLifecycleStore(db)
	lifecycleSvc := lifecycle.NewService(domainStore, lifecycleStore, logger)
	domainHandler := handler.NewDomainHandler(lifecycleSvc, logger)

	templateStore := postgres.NewTemplateStore(db)
	templateSvc := tmplsvc.NewService(templateStore, logger)
	templateHandler := handler.NewTemplateHandler(templateSvc, logger)

	artifactStore := postgres.NewArtifactStore(db)
	artifactHandler := handler.NewArtifactHandler(artifactStore, logger)

	releaseStore := postgres.NewReleaseStore(db)
	domainTaskStore := postgres.NewDomainTaskStore(db)
	agentStore := postgres.NewAgentStore(db)
	rollbackStore := postgres.NewRollbackStore(db)
	hostGroupStore := postgres.NewHostGroupStore(db)
	pkgStorage := pkgstorage.NewMinIOStorage(storageClient, cfg.Storage.ArtifactsBucket)
	releaseSvc := release.NewService(releaseStore, domainStore, templateStore, agentStore, artifactStore, domainTaskStore, rollbackStore, hostGroupStore, pkgStorage, asynqClient, logger)
	releaseHandler := handler.NewReleaseHandler(releaseSvc, logger)
	hostGroupHandler := handler.NewHostGroupHandler(hostGroupStore, logger)

	agentSvc := agentsvc.NewService(agentStore, logger)
	agentProtocolHandler := handler.NewAgentProtocolHandler(agentSvc, logger)
	agentHandler := handler.NewAgentHandler(agentSvc, logger)

	registrarStore := postgres.NewRegistrarStore(db)
	registrarSvc := registrar.NewService(registrarStore, logger)
	registrarHandler := handler.NewRegistrarHandler(registrarSvc, logger)

	dnsProviderStore := postgres.NewDNSProviderStore(db)
	dnsProviderSvc := dnsprovider.NewService(dnsProviderStore, logger)
	dnsProviderHandler := handler.NewDNSProviderHandler(dnsProviderSvc, logger)

	sslCertStore := postgres.NewSSLCertificateStore(db)
	sslSvc := sslsvc.NewService(sslCertStore, domainStore, logger)
	sslHandler := handler.NewSSLHandler(sslSvc, logger)

	costStore := postgres.NewCostStore(db)
	costSvc := costsvc.NewService(costStore, domainStore, registrarStore, logger)
	costHandler := handler.NewCostHandler(costSvc, logger)

	tagStore := postgres.NewTagStore(db)
	tagSvc := tagsvc.NewService(tagStore, domainStore, logger)
	tagHandler := handler.NewTagHandler(tagSvc, logger)

	expirySvc := domainsvc.NewExpiryService(domainStore, logger)
	expiryHandler := handler.NewExpiryHandler(expirySvc, logger)

	importJobStore := postgres.NewImportJobStore(db)
	importSvc := importsvc.NewService(importJobStore, domainStore, lifecycleSvc, asynqClient, logger)
	importHandler := handler.NewImportHandler(importSvc, logger)

	dnsQuerySvc := dnsquery.NewService("", logger) // "" = auto-detect system resolver
	dnsQueryHandler := handler.NewDNSQueryHandler(dnsQuerySvc, lifecycleSvc, dnsProviderStore, logger)

	dnsRecordSvc := dnsrecsvc.NewService(dnsProviderStore, domainStore, logger)
	dnsRecordHandler := handler.NewDNSRecordHandler(dnsRecordSvc, lifecycleSvc, logger)

	// ── Management API listener (:8080, JWT auth) ──────────────────────────
	mgmtRouter := buildManagementRouter(logger, router.Deps{
		AuthHandler:        authHandler,
		ProjectHandler:     projectHandler,
		DomainHandler:      domainHandler,
		TemplateHandler:    templateHandler,
		ArtifactHandler:    artifactHandler,
		ReleaseHandler:     releaseHandler,
		AgentHandler:       agentHandler,
		HostGroupHandler:   hostGroupHandler,
		RegistrarHandler:   registrarHandler,
		DNSProviderHandler: dnsProviderHandler,
		SSLHandler:         sslHandler,
		CostHandler:        costHandler,
		TagHandler:         tagHandler,
		ExpiryHandler:      expiryHandler,
		ImportHandler:      importHandler,
		DNSQueryHandler:    dnsQueryHandler,
		DNSRecordHandler:   dnsRecordHandler,
		JWTManager:         jwtMgr,
	})
	mgmtAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	mgmtServer := &http.Server{
		Addr:         mgmtAddr,
		Handler:      mgmtRouter,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Agent protocol listener (:8443, mTLS) ─────────────────────────────
	agentRouter := buildAgentRouter(logger, agentProtocolHandler)
	agentAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.AgentPort)
	agentServer := &http.Server{
		Addr:         agentAddr,
		Handler:      agentRouter,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if cfg.Server.CACertFile != "" {
		tlsCfg, tlsErr := buildAgentTLSConfig(cfg.Server)
		if tlsErr != nil {
			logger.Fatal("build agent TLS config", zap.Error(tlsErr))
		}
		agentServer.TLSConfig = tlsCfg
	}

	// ── Start both listeners ───────────────────────────────────────────────
	errCh := make(chan error, 2)

	go func() {
		logger.Info("management API listening", zap.String("addr", mgmtAddr))
		if err := mgmtServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("mgmt server: %w", err)
		}
	}()

	go func() {
		logger.Info("agent protocol listening", zap.String("addr", agentAddr))
		var serveErr error
		if agentServer.TLSConfig != nil {
			serveErr = agentServer.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		} else {
			// Dev mode: plain HTTP on agent port (no mTLS certs configured)
			serveErr = agentServer.ListenAndServe()
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- fmt.Errorf("agent server: %w", serveErr)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-errCh:
		logger.Error("server error", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := mgmtServer.Shutdown(ctx); err != nil {
		logger.Error("management server shutdown error", zap.Error(err))
	}
	if err := agentServer.Shutdown(ctx); err != nil {
		logger.Error("agent server shutdown error", zap.Error(err))
	}

	logger.Info("server exited cleanly")
}

// buildManagementRouter returns the Gin engine for the JWT-authenticated management API.
func buildManagementRouter(_ *zap.Logger, deps router.Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.RegisterV1(r, deps)

	return r
}

// buildAgentRouter returns the Gin engine for the mTLS Pull Agent protocol.
// Endpoints: /agent/v1/register, /agent/v1/heartbeat, /agent/v1/tasks, etc.
func buildAgentRouter(_ *zap.Logger, h *handler.AgentProtocolHandler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Agent protocol routes — mTLS enforced at TLS layer; middleware extracts cert info
	v1 := r.Group("/agent/v1", middleware.AgentMTLS())
	{
		v1.POST("/register", h.Register)
		v1.POST("/heartbeat", h.Heartbeat)
		v1.GET("/tasks", h.PollTasks)
		v1.POST("/tasks/:taskId/claim", h.ClaimTask)
		v1.POST("/tasks/:taskId/report", h.ReportTask)
	}

	return r
}

// buildAgentTLSConfig constructs a tls.Config that requires client certificates
// signed by the Agent CA, enforcing the mTLS safety boundary for the agent protocol.
func buildAgentTLSConfig(cfg bootstrap.ServerConfig) (*tls.Config, error) {
	caCert, err := os.ReadFile(cfg.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("parse CA cert")
	}
	return &tls.Config{
		ClientCAs:  pool,
		ClientAuth: tls.RequireAndVerifyClientCert,
		MinVersion: tls.VersionTLS13,
	}, nil
}
