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
	TypeProbeRunL1 = "probe:run_l1"
	TypeProbeRunL2 = "probe:run_l2"
	TypeProbeRunL3 = "probe:run_l3"

	// SSL certificate checking
	TypeSSLCheckExpiry    = "ssl:check_expiry"     // single domain TLS probe + upsert
	TypeSSLCheckAllActive = "ssl:check_all_active" // batch: enqueue TypeSSLCheckExpiry per active domain

	// Notify
	TypeNotifySend = "notify:send"
)

