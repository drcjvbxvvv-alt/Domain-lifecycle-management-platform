package bootstrap

import (
	"github.com/hibiken/asynq"
)

// Queues defines the canonical asynq queue names and their priority weights.
// Higher number = higher priority. Must match CLAUDE.md §"asynq Queue Layout" exactly.
// strict: false — weighted priority dispatch (not strict priority).
var Queues = map[string]int{
	"critical":  10, // release:rollback, agent:health_check escalation
	"release":   6,  // release:plan, release:dispatch_shard, release:finalize
	"artifact":  5,  // artifact:build, artifact:sign
	"lifecycle": 4,  // lifecycle:provision, lifecycle:deprovision
	"probe":     3,  // probe:run_l1/l2/l3
	"default":   2,  // notify:send, misc
}

// QueueForTask maps each task type to its canonical queue name.
// cmd/worker/main.go MUST NOT override these — all enqueue calls
// must use asynq.Queue(QueueForTask[taskType]).
var QueueForTask = map[string]string{
	// critical
	"release:rollback":    "critical",
	"agent:health_check":  "critical",
	// release
	"release:plan":           "release",
	"release:dispatch_shard": "release",
	"release:probe_verify":   "release",
	"release:finalize":       "release",
	// artifact
	"artifact:build": "artifact",
	"artifact:sign":  "artifact",
	// lifecycle
	"lifecycle:provision":   "lifecycle",
	"lifecycle:deprovision": "lifecycle",
	// probe
	"probe:run_l1": "probe",
	"probe:run_l2": "probe",
	"probe:run_l3": "probe",
	// default
	"notify:send":            "default",
	"agent:upgrade_dispatch": "default",
}

// NewAsynqClient returns an asynq.Client connected to Redis.
func NewAsynqClient(cfg RedisConfig) *asynq.Client {
	return asynq.NewClient(asynq.RedisClientOpt{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
}

// DefaultWorkerConcurrency is the total number of concurrent worker goroutines.
// Sized to cover the sum of per-queue concurrency from CLAUDE.md §"asynq Queue Layout":
// critical(20) + release(10) + artifact(5) + lifecycle(10) + probe(20) + default(10) = 75.
const DefaultWorkerConcurrency = 75

// NewAsynqServer returns a configured asynq.Server with the canonical queue layout.
// Pass concurrency=0 to use DefaultWorkerConcurrency.
func NewAsynqServer(cfg RedisConfig, concurrency int) *asynq.Server {
	if concurrency <= 0 {
		concurrency = DefaultWorkerConcurrency
	}
	return asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		},
		asynq.Config{
			Concurrency: concurrency,
			Queues:      Queues,
			// StrictPriority: false — weighted priority (default), not strict FIFO per queue
		},
	)
}

// NewAsynqInspector returns an asynq.Inspector for queue monitoring.
func NewAsynqInspector(cfg RedisConfig) *asynq.Inspector {
	return asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
}
