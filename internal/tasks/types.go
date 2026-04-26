package tasks

// Task type constants for the asynq task queue.
// Each constant maps to a specific handler registered in cmd/worker/main.go.
const (
	// Domain lifecycle
	TypeLifecycleProvision   = "lifecycle:provision"
	TypeLifecycleDeprovision = "lifecycle:deprovision"

	// Artifact build
	TypeArtifactBuild = "artifact:build"
	TypeArtifactSign  = "artifact:sign"

	// Release execution
	TypeReleasePlan          = "release:plan"
	TypeReleaseDispatchShard = "release:dispatch_shard"
	TypeReleaseProbeVerify   = "release:probe_verify"
	TypeReleaseFinalize      = "release:finalize"
	TypeReleaseRollback      = "release:rollback"

	// Agent management
	TypeAgentHealthCheck     = "agent:health_check"
	TypeAgentUpgradeDispatch = "agent:upgrade_dispatch"

	// Probe
	TypeProbeScheduleAll = "probe:schedule_all" // batch: create probe_tasks for all enabled policies
	TypeProbeRunL1       = "probe:run_l1"       // single domain L1 check (DNS+TCP+HTTP+TLS)
	TypeProbeRunL2       = "probe:run_l2"       // single domain L2 check (keyword+meta+hash)
	TypeProbeRunL3       = "probe:run_l3"       // single domain L3 check (health endpoint)

	// Domain expiry checking
	TypeDomainExpiryCheck = "domain:expiry_check" // daily: compute expiry_status, detect changes, notify

	// SSL certificate checking
	TypeSSLCheckExpiry    = "ssl:check_expiry"     // single domain TLS probe + upsert
	TypeSSLCheckAllActive = "ssl:check_all_active" // batch: enqueue TypeSSLCheckExpiry per active domain

	// Domain import
	TypeDomainImport = "domain:import" // process a domain_import_jobs row row-by-row

	// DNS drift monitoring
	TypeDNSDriftCheckAll = "dns:drift_check_all" // batch: enqueue TypeDNSDriftCheck per active domain with provider
	TypeDNSDriftCheck    = "dns:drift_check"     // single domain: run drift check, alert on deviation

	// NS delegation verification (B.2)
	TypeNSCheckAll = "domain:ns_check_all" // batch: enqueue TypeNSCheck for all pending/mismatch domains
	TypeNSCheck    = "domain:ns_check"     // single domain: verify NS delegation, update status

	// Alert engine (PC.2)
	TypeAlertFire = "alert:fire" // persist alert_event + dedup + enqueue notify:send

	// Notify
	TypeNotifySend = "notify:send"
)

