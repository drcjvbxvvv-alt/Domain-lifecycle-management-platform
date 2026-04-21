package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ApplyRateLimitWindow is the cooldown period between apply operations
// for the same domain. Prevents rapid-fire zone mutations.
const ApplyRateLimitWindow = 5 * time.Minute

// ApplyRateLimiter enforces a per-domain cooldown for DNS apply operations.
// Uses Redis SET NX EX for atomic check-and-set.
type ApplyRateLimiter struct {
	rdb *redis.Client
}

// NewApplyRateLimiter creates a rate limiter backed by the given Redis client.
func NewApplyRateLimiter(rdb *redis.Client) *ApplyRateLimiter {
	return &ApplyRateLimiter{rdb: rdb}
}

// Allow checks whether an apply is permitted for the given domain.
// Returns true if allowed (key set successfully), false if rate-limited.
// On Redis error, fails open (allows the apply) to avoid blocking operations.
func (r *ApplyRateLimiter) Allow(ctx context.Context, domainID int64) bool {
	key := fmt.Sprintf("dns:apply:cooldown:%d", domainID)
	ok, err := r.rdb.SetNX(ctx, key, 1, ApplyRateLimitWindow).Result()
	if err != nil {
		// Fail open — don't block DNS operations because Redis is unavailable.
		return true
	}
	return ok
}

// Remaining returns the time remaining before the next apply is permitted.
// Returns 0 if the domain is not rate-limited (or on error).
func (r *ApplyRateLimiter) Remaining(ctx context.Context, domainID int64) time.Duration {
	key := fmt.Sprintf("dns:apply:cooldown:%d", domainID)
	ttl, err := r.rdb.TTL(ctx, key).Result()
	if err != nil || ttl < 0 {
		return 0
	}
	return ttl
}
