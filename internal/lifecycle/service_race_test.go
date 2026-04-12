//go:build integration

package lifecycle_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"domain-platform/internal/lifecycle"
	"domain-platform/store/postgres"
)

// testDSN returns a PostgreSQL DSN for integration tests.
// Override with TEST_DATABASE_DSN env var if needed.
func testDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_DSN"); dsn != "" {
		return dsn
	}
	return "host=localhost port=5432 dbname=domain_platform user=postgres password=postgres sslmode=disable"
}

// TestTransition_RaceCondition verifies the SELECT FOR UPDATE + optimistic
// check race-safety property. 10 goroutines attempt the same
// requested → approved transition concurrently. Exactly 1 must succeed;
// the other 9 must receive ErrLifecycleRaceCondition. The history table
// must contain exactly 1 row for this transition.
//
// Run: go test -tags integration -race -count=50 ./internal/lifecycle/...
func TestTransition_RaceCondition(t *testing.T) {
	db, err := sqlx.Connect("postgres", testDSN())
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to postgres: %v", err)
	}
	defer db.Close()

	logger := zap.NewNop()
	domainStore := postgres.NewDomainStore(db)
	lifecycleStore := postgres.NewLifecycleStore(db)
	svc := lifecycle.NewService(domainStore, lifecycleStore, logger)

	ctx := context.Background()

	// Ensure a project exists for the FK constraint.
	// Use a unique slug per test run to avoid conflicts with -count=50.
	slug := fmt.Sprintf("rt%d%d", time.Now().UnixNano()%1000000, rand.Int63()%1000000)
	var projectID int64
	err = db.GetContext(ctx, &projectID,
		`INSERT INTO projects (name, slug, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW()) RETURNING id`, slug, slug)
	require.NoError(t, err, "create test project")
	defer db.ExecContext(ctx, "DELETE FROM projects WHERE id = $1", projectID)

	// Create a test domain in "requested" state
	d, err := svc.Register(ctx, lifecycle.RegisterInput{
		ProjectID:   projectID,
		FQDN:        fmt.Sprintf("race-%d-%d.example.com", time.Now().UnixNano(), rand.Int63()),
		TriggeredBy: "test",
	})
	require.NoError(t, err, "register domain")
	// Cleanup after test
	defer func() {
		db.ExecContext(ctx, "DELETE FROM domain_lifecycle_history WHERE domain_id = $1", d.ID)
		db.ExecContext(ctx, "DELETE FROM domains WHERE id = $1", d.ID)
	}()

	const goroutines = 10
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		wins     int
		raceErrs int
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			err := svc.Transition(ctx, d.ID, "requested", "approved", "race test", "test")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if err == lifecycle.ErrLifecycleRaceCondition {
				raceErrs++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, wins, "exactly one goroutine must win the transition")
	assert.Equal(t, goroutines-1, raceErrs, "all other goroutines must get race condition error")

	// Verify exactly 1 history row for the requested → approved transition
	rows, err := svc.GetHistory(ctx, d.ID)
	require.NoError(t, err)

	approvedCount := 0
	for _, row := range rows {
		if row.ToState == "approved" {
			approvedCount++
		}
	}
	assert.Equal(t, 1, approvedCount, "exactly 1 history row for the approved transition")

	// Verify final state is approved
	updated, err := svc.GetByID(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, "approved", updated.LifecycleState)
}
