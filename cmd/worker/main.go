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

	alertsvc "domain-platform/internal/alert"
	"domain-platform/internal/artifact"
	"domain-platform/internal/bootstrap"
	domainsvc "domain-platform/internal/domain"
	"domain-platform/internal/dnsquery"
	"domain-platform/internal/dnsrecord"
	importsvc "domain-platform/internal/importer"
	"domain-platform/internal/lifecycle"
	"domain-platform/internal/probe"
	"domain-platform/internal/release"
	sslsvc "domain-platform/internal/ssl"
	"domain-platform/internal/tasks"
	"domain-platform/pkg/notify"
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

	// Domain expiry check + notification
	expirySvc := domainsvc.NewExpiryService(domainStore, logger)
	// Build notifier chain from config (Telegram + Webhook). Falls back to Noop.
	var notifier notify.Notifier = notify.NewNoop()
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		tg := notify.NewTelegram(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
		if cfg.Webhook.URL != "" {
			notifier = notify.NewMulti(tg, notify.NewWebhook(cfg.Webhook.URL))
		} else {
			notifier = tg
		}
	} else if cfg.Webhook.URL != "" {
		notifier = notify.NewWebhook(cfg.Webhook.URL)
	}
	expiryCheckHandler := domainsvc.NewHandleExpiryCheck(expirySvc, notifier, logger)
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

	// ── Alert engine (PC.2) + Notification dispatcher (PC.6) ─────────────
	alertStore := postgres.NewAlertStore(db)
	notifStore := postgres.NewNotificationStore(db)
	alertEngine := alertsvc.NewEngine(alertStore, asynqClient, logger)
	dispatcher := alertsvc.NewDispatcher(notifStore, alertStore, logger)
	alertFireHandler := alertsvc.NewHandleAlertFire(alertEngine, logger)
	notifySendHandler := alertsvc.NewHandleNotifySend(dispatcher, alertStore, logger)

	// ── Probe engine (PC.1) ───────────────────────────────────────────────
	probeStore := postgres.NewProbeStore(db)
	probeSvc := probe.NewService(probeStore, domainStore, asynqClient, logger)
	probeScheduleAllHandler := probe.NewHandleScheduleAll(probeSvc, logger)
	probeL1Handler := probe.NewHandleL1(probeSvc, logger)
	probeL2Handler := probe.NewHandleL2(probeSvc, logger)
	probeL3Handler := probe.NewHandleL3(probeSvc, logger)

	// DNS drift monitoring
	dnsQuerySvc := dnsquery.NewService("", logger) // "" = auto-detect system resolver
	dnsProviderStore := postgres.NewDNSProviderStore(db)
	driftCheckAllHandler := dnsquery.NewHandleDriftCheckAll(domainStore, asynqClient, logger)
	driftCheckHandler := dnsquery.NewHandleDriftCheck(dnsQuerySvc, domainStore, dnsProviderStore, notifier, rdb, logger)

	// NS delegation verification (B.2)
	dnsBindingSvc := dnsrecord.NewService(dnsProviderStore, domainStore, logger)
	nsCheckAllHandler := dnsrecord.NewHandleNSCheckAll(domainStore, dnsProviderStore, dnsBindingSvc, asynqClient, logger)
	nsCheckHandler := dnsrecord.NewHandleNSCheck(domainStore, notifier, rdb, logger)

	lifecycleStore := postgres.NewLifecycleStore(db)
	lifecycleSvc := lifecycle.NewService(domainStore, lifecycleStore, logger)
	importJobStore := postgres.NewImportJobStore(db)
	importSvc := importsvc.NewService(importJobStore, domainStore, lifecycleSvc, asynqClient, logger)

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
	mux.Handle(tasks.TypeDomainExpiryCheck, expiryCheckHandler)
	mux.HandleFunc(tasks.TypeDomainImport, importSvc.HandleDomainImport)
	mux.Handle(tasks.TypeDNSDriftCheckAll, driftCheckAllHandler)
	mux.Handle(tasks.TypeDNSDriftCheck, driftCheckHandler)
	mux.HandleFunc(tasks.TypeNSCheckAll, nsCheckAllHandler.ProcessTask)
	mux.HandleFunc(tasks.TypeNSCheck, nsCheckHandler.ProcessTask)

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
	mux.HandleFunc(tasks.TypeProbeScheduleAll, probeScheduleAllHandler.ProcessTask)
	mux.HandleFunc(tasks.TypeProbeRunL1, probeL1Handler.ProcessTask)
	mux.HandleFunc(tasks.TypeProbeRunL2, probeL2Handler.ProcessTask)
	mux.HandleFunc(tasks.TypeProbeRunL3, probeL3Handler.ProcessTask)
	mux.HandleFunc(tasks.TypeAlertFire, alertFireHandler.ProcessTask)
	mux.HandleFunc(tasks.TypeNotifySend, notifySendHandler.ProcessTask)

	// ── Periodic scheduler ────────────────────────────────────────────────────
	// Uses asynq.Scheduler to enqueue recurring tasks at fixed intervals.
	// The scheduler runs in a separate goroutine alongside the worker server.
	scheduler := asynq.NewScheduler(
		asynq.RedisClientOpt{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		},
		&asynq.SchedulerOpts{},
	)

	// DNS drift check: every 30 minutes.
	// The batch task enqueues one TypeDNSDriftCheck per active domain with a provider.
	driftPayload, _ := json.Marshal(tasks.DNSDriftCheckAllPayload{})
	driftTask := asynq.NewTask(tasks.TypeDNSDriftCheckAll, driftPayload, asynq.Queue("default"))
	if _, err := scheduler.Register("@every 30m", driftTask); err != nil {
		logger.Fatal("register dns drift scheduler", zap.Error(err))
	}

	// NS delegation check: every hour.
	// The batch task enqueues one TypeNSCheck per domain with pending/mismatch NS.
	nsCheckAllPayload, _ := json.Marshal(tasks.NSCheckAllPayload{})
	nsCheckAllTask := asynq.NewTask(tasks.TypeNSCheckAll, nsCheckAllPayload, asynq.Queue("default"))
	if _, err := scheduler.Register("@every 1h", nsCheckAllTask); err != nil {
		logger.Fatal("register ns check_all scheduler", zap.Error(err))
	}

	// Probe schedule-all: every 5 minutes.
	// Creates probe_tasks for all enabled policies × active domains, then enqueues tier runners.
	probeSchedulePayload := []byte(`{}`)
	probeScheduleTask := asynq.NewTask(tasks.TypeProbeScheduleAll, probeSchedulePayload, asynq.Queue("probe"))
	if _, err := scheduler.Register("@every 5m", probeScheduleTask); err != nil {
		logger.Fatal("register probe schedule_all scheduler", zap.Error(err))
	}

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

	go func() {
		if err := scheduler.Run(); err != nil {
			logger.Fatal("scheduler run error", zap.Error(err))
		}
	}()

	<-quit
	logger.Info("shutdown signal received — draining worker")
	scheduler.Shutdown()
	srv.Shutdown()
	logger.Info("worker exited cleanly")
}
