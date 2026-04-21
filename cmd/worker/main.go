package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/artifact"
	"domain-platform/internal/bootstrap"
	"domain-platform/internal/release"
	sslsvc "domain-platform/internal/ssl"
	"domain-platform/internal/tasks"
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

	storageClient, err := bootstrap.NewStorage(cfg.Storage)
	if err != nil {
		logger.Fatal("connect storage", zap.Error(err))
	}

	// ── Build real task handlers ───────────────────────────────────────────
	artifactStore := postgres.NewArtifactStore(db)
	domainStore := postgres.NewDomainStore(db)
	sslCertStore := postgres.NewSSLCertificateStore(db)
	sslService := sslsvc.NewService(sslCertStore, domainStore, logger)
	sslCheckHandler := sslsvc.NewHandleCheckExpiry(sslService, logger)
	sslCheckAllHandler := sslsvc.NewHandleCheckAllActive(sslService, logger)
	templateStore := postgres.NewTemplateStore(db)
	agentStore := postgres.NewAgentStore(db)
	releaseStore := postgres.NewReleaseStore(db)
	domainTaskStore := postgres.NewDomainTaskStore(db)
	rollbackStore := postgres.NewRollbackStore(db)
	hostGroupStore := postgres.NewHostGroupStore(db)
	jwtCfg := cfg.JWT

	signingKey := jwtCfg.Secret // reuse JWT secret as HMAC signing key for P1
	signer := artifact.NewHMACSigner(signingKey)

	minioStorage := pkgstorage.NewMinIOStorage(storageClient, cfg.Storage.ArtifactsBucket)

	asynqClient := bootstrap.NewAsynqClient(cfg.Redis)
	defer asynqClient.Close()

	// Release service (needed by release handlers + artifact build callback)
	releaseSvc := release.NewService(
		releaseStore, domainStore, templateStore, agentStore, artifactStore,
		domainTaskStore, rollbackStore, hostGroupStore, minioStorage, asynqClient, logger,
	)

	builder := artifact.NewBuilder(artifactStore, templateStore, minioStorage, signer, logger)
	artifactBuildHandler := artifact.NewHandleBuild(builder, domainStore, templateStore, releaseSvc.MarkReady, logger)

	// Release asynq handlers
	releasePlanHandler := release.NewHandlePlan(releaseSvc, logger)
	releaseDispatchHandler := release.NewHandleDispatchShard(releaseSvc, logger)
	releaseFinalizeHandler := release.NewHandleFinalize(releaseSvc, logger)
	releaseRollbackHandler := release.NewHandleRollback(releaseSvc, logger)

	// ── asynq server with canonical queue layout ───────────────────────────
	// Queue priorities: critical(10) > release(6) > artifact(5) > lifecycle(4) > probe(3) > default(2)
	// Total concurrency: 75 goroutines (sum of per-queue targets in CLAUDE.md §Queue Layout)
	srv := bootstrap.NewAsynqServer(cfg.Redis, 0)

	mux := asynq.NewServeMux()

	// ── Real handlers ──────────────────────────────────────────────────────
	mux.Handle(tasks.TypeArtifactBuild, artifactBuildHandler)
	mux.Handle(tasks.TypeSSLCheckExpiry, sslCheckHandler)
	mux.Handle(tasks.TypeSSLCheckAllActive, sslCheckAllHandler)

	// ── Stub handlers (log payload, return nil) ───────────────────────────
	// These will be replaced by real implementations in P2+.
	stubFor := func(name string) asynq.HandlerFunc {
		return func(ctx context.Context, t *asynq.Task) error {
			var raw json.RawMessage = t.Payload()
			logger.Info("stub task handler — noop",
				zap.String("type", name),
				zap.ByteString("payload", raw),
			)
			return nil
		}
	}

	mux.HandleFunc(tasks.TypeLifecycleProvision, stubFor(tasks.TypeLifecycleProvision))
	mux.HandleFunc(tasks.TypeLifecycleDeprovision, stubFor(tasks.TypeLifecycleDeprovision))
	mux.HandleFunc(tasks.TypeArtifactSign, stubFor(tasks.TypeArtifactSign))
	mux.Handle(tasks.TypeReleasePlan, releasePlanHandler)
	mux.Handle(tasks.TypeReleaseDispatchShard, releaseDispatchHandler)
	mux.HandleFunc(tasks.TypeReleaseProbeVerify, stubFor(tasks.TypeReleaseProbeVerify))
	mux.Handle(tasks.TypeReleaseFinalize, releaseFinalizeHandler)
	mux.Handle(tasks.TypeReleaseRollback, releaseRollbackHandler)
	mux.HandleFunc(tasks.TypeAgentHealthCheck, stubFor(tasks.TypeAgentHealthCheck))
	mux.HandleFunc(tasks.TypeAgentUpgradeDispatch, stubFor(tasks.TypeAgentUpgradeDispatch))
	mux.HandleFunc(tasks.TypeProbeRunL1, stubFor(tasks.TypeProbeRunL1))
	mux.HandleFunc(tasks.TypeProbeRunL2, stubFor(tasks.TypeProbeRunL2))
	mux.HandleFunc(tasks.TypeProbeRunL3, stubFor(tasks.TypeProbeRunL3))
	mux.HandleFunc(tasks.TypeNotifySend, stubFor(tasks.TypeNotifySend))

	logger.Info("worker starting",
		zap.String("redis", cfg.Redis.Addr),
		zap.Int("concurrency", bootstrap.DefaultWorkerConcurrency),
		zap.Any("queues", bootstrap.Queues),
	)

	// Run blocks until the server is stopped.
	// Graceful shutdown is handled by asynq.Server.Shutdown().
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Run(mux); err != nil {
			logger.Fatal("worker run error", zap.Error(err))
		}
	}()

	<-quit
	logger.Info("shutdown signal received — draining worker")
	srv.Shutdown()
	logger.Info("worker exited cleanly")
}
