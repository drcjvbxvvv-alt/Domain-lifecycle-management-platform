package gfw

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	confidenceTTL    = 2 * time.Hour
	confidenceKeyFmt = "gfw:confidence:%d" // keyed by domain_id
)

// confidenceState is persisted in Redis / in-memory as JSON.
type confidenceState struct {
	Count        int      `json:"count"`         // consecutive blocking measurements
	UniqueNodes  []string `json:"unique_nodes"`  // deduplicated node IDs
	LastBlocking string   `json:"last_blocking"` // last blocking type seen
}

// scoreFromState derives a 0.0–1.0 confidence from accumulated state.
//
// Scoring table:
//
//	count=1, nodes=1 → 0.30  (single data point)
//	count=2, nodes=1 → 0.50  (repeated from same node)
//	count≥1, nodes≥2 → 0.70  (corroborated by multiple nodes)
//	count≥3, nodes≥2 → 0.90  (high-confidence persistent blocking)
func scoreFromState(s confidenceState) float64 {
	nodes := len(uniqueStrings(s.UniqueNodes))
	switch {
	case s.Count >= 3 && nodes >= 2:
		return 0.90
	case nodes >= 2:
		return 0.70
	case s.Count >= 2:
		return 0.50
	case s.Count >= 1:
		return 0.30
	default:
		return 0.00
	}
}

// uniqueStrings returns a deduplicated slice preserving order.
func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// ConfidenceTracker records consecutive blocking observations for a domain
// and returns a confidence score (0.0–1.00) that reflects how certain we are
// about the verdict.
type ConfidenceTracker interface {
	// Record records one blocking observation for domainID from nodeID.
	// If blocking is empty ("") the counter is reset to zero (domain became
	// accessible again).
	// Returns the updated confidence score.
	Record(ctx context.Context, domainID int64, nodeID string, blocking string) (float64, error)

	// Score returns the current confidence score without modifying state.
	Score(ctx context.Context, domainID int64) (float64, error)

	// Reset clears the tracker state for a domain.
	Reset(ctx context.Context, domainID int64) error
}

// ─── Redis implementation ─────────────────────────────────────────────────────

// RedisConfidenceTracker stores state in Redis with a TTL.
type RedisConfidenceTracker struct {
	rdb *redis.Client
}

// NewRedisConfidenceTracker creates a tracker backed by the given Redis client.
func NewRedisConfidenceTracker(rdb *redis.Client) *RedisConfidenceTracker {
	return &RedisConfidenceTracker{rdb: rdb}
}

func (t *RedisConfidenceTracker) key(domainID int64) string {
	return fmt.Sprintf(confidenceKeyFmt, domainID)
}

func (t *RedisConfidenceTracker) load(ctx context.Context, domainID int64) (confidenceState, error) {
	raw, err := t.rdb.Get(ctx, t.key(domainID)).Bytes()
	if err == redis.Nil {
		return confidenceState{}, nil
	}
	if err != nil {
		return confidenceState{}, fmt.Errorf("confidence load domain %d: %w", domainID, err)
	}
	var s confidenceState
	if err := json.Unmarshal(raw, &s); err != nil {
		return confidenceState{}, fmt.Errorf("confidence unmarshal domain %d: %w", domainID, err)
	}
	return s, nil
}

func (t *RedisConfidenceTracker) save(ctx context.Context, domainID int64, s confidenceState) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("confidence marshal domain %d: %w", domainID, err)
	}
	if err := t.rdb.Set(ctx, t.key(domainID), raw, confidenceTTL).Err(); err != nil {
		return fmt.Errorf("confidence save domain %d: %w", domainID, err)
	}
	return nil
}

func (t *RedisConfidenceTracker) Record(ctx context.Context, domainID int64, nodeID string, blocking string) (float64, error) {
	s, err := t.load(ctx, domainID)
	if err != nil {
		return 0, err
	}

	if blocking == "" {
		// Domain is accessible — reset state
		s = confidenceState{}
	} else {
		if blocking != s.LastBlocking && s.LastBlocking != "" {
			// Blocking type changed — reset count but keep nodes
			s.Count = 0
		}
		s.Count++
		s.UniqueNodes = uniqueStrings(append(s.UniqueNodes, nodeID))
		s.LastBlocking = blocking
	}

	if err := t.save(ctx, domainID, s); err != nil {
		return 0, err
	}
	return scoreFromState(s), nil
}

func (t *RedisConfidenceTracker) Score(ctx context.Context, domainID int64) (float64, error) {
	s, err := t.load(ctx, domainID)
	if err != nil {
		return 0, err
	}
	return scoreFromState(s), nil
}

func (t *RedisConfidenceTracker) Reset(ctx context.Context, domainID int64) error {
	if err := t.rdb.Del(ctx, t.key(domainID)).Err(); err != nil && err != redis.Nil {
		return fmt.Errorf("confidence reset domain %d: %w", domainID, err)
	}
	return nil
}

// ─── In-memory implementation (for tests and standalone probe binaries) ──────

// MemoryConfidenceTracker is a thread-safe in-memory implementation.
type MemoryConfidenceTracker struct {
	mu    sync.Mutex
	store map[int64]confidenceState
}

// NewMemoryConfidenceTracker returns an in-memory tracker.
func NewMemoryConfidenceTracker() *MemoryConfidenceTracker {
	return &MemoryConfidenceTracker{store: make(map[int64]confidenceState)}
}

func (t *MemoryConfidenceTracker) Record(_ context.Context, domainID int64, nodeID string, blocking string) (float64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.store[domainID]
	if blocking == "" {
		s = confidenceState{}
	} else {
		if blocking != s.LastBlocking && s.LastBlocking != "" {
			s.Count = 0
		}
		s.Count++
		s.UniqueNodes = uniqueStrings(append(s.UniqueNodes, nodeID))
		s.LastBlocking = blocking
	}
	t.store[domainID] = s
	return scoreFromState(s), nil
}

func (t *MemoryConfidenceTracker) Score(_ context.Context, domainID int64) (float64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return scoreFromState(t.store[domainID]), nil
}

func (t *MemoryConfidenceTracker) Reset(_ context.Context, domainID int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.store, domainID)
	return nil
}
