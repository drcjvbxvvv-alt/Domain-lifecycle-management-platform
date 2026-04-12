//go:build integration

package release_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"domain-platform/internal/release"
	"domain-platform/store/postgres"
)

func testDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_DSN"); dsn != "" {
		return dsn
	}
	return "host=localhost port=5432 dbname=domain_platform user=postgres password=postgres sslmode=disable"
}

// TestTransitionRelease_RaceCondition verifies the SELECT FOR UPDATE + optimistic
// check race-safety property. 10 goroutines attempt the same
// pending → planning transition concurrently. Exactly 1 must succeed.
//
// Run: go test -tags integration -race -count=50 ./internal/release/...
func TestTransitionRelease_RaceCondition(t *testing.T) {
	db, err := sqlx.Connect("postgres", testDSN())
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to postgres: %v", err)
	}
	defer db.Close()

	logger := zap.NewNop()
	releaseStore := postgres.NewReleaseStore(db)
	domainStore := postgres.NewDomainStore(db)
	tmplStore := postgres.NewTemplateStore(db)

	// Create a throwaway asynq client that doesn't actually connect
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: "localhost:6379"})
	defer asynqClient.Close()

	svc := release.NewService(releaseStore, domainStore, tmplStore, asynqClient, logger)
	ctx := context.Background()

	// Create a test project
	slug := fmt.Sprintf("rr%d%d", time.Now().UnixNano()%1000000, rand.Int63()%1000000)
	var projectID int64
	err = db.GetContext(ctx, &projectID,
		`INSERT INTO projects (name, slug, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW()) RETURNING id`, slug, slug)
	require.NoError(t, err)
	defer db.ExecContext(ctx, "DELETE FROM projects WHERE id = $1", projectID)

	// Create a template + published version for the FK
	var tmplID int64
	err = db.GetContext(ctx, &tmplID,
		`INSERT INTO templates (project_id, name, kind) VALUES ($1, $2, 'html') RETURNING id`,
		projectID, slug)
	require.NoError(t, err)
	defer db.ExecContext(ctx, "DELETE FROM templates WHERE id = $1", tmplID)

	var tmplVerID int64
	err = db.GetContext(ctx, &tmplVerID,
		`INSERT INTO template_versions (template_id, version_label, checksum, default_variables, published_at)
		 VALUES ($1, 'v1', 'test', '{}', NOW()) RETURNING id`, tmplID)
	require.NoError(t, err)
	defer db.ExecContext(ctx, "DELETE FROM template_versions WHERE id = $1", tmplVerID)

	// Create a release in "pending" state
	releaseIDStr := fmt.Sprintf("race-%d-%d", time.Now().UnixNano(), rand.Int63())
	var releaseDBID int64
	err = db.GetContext(ctx, &releaseDBID,
		`INSERT INTO releases (release_id, project_id, template_version_id, release_type, trigger_source)
		 VALUES ($1, $2, $3, 'html', 'test') RETURNING id`,
		releaseIDStr[:16], projectID, tmplVerID)
	require.NoError(t, err)
	defer func() {
		db.ExecContext(ctx, "DELETE FROM release_state_history WHERE release_id = $1", releaseDBID)
		db.ExecContext(ctx, "DELETE FROM releases WHERE id = $1", releaseDBID)
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
			err := svc.TransitionRelease(ctx, releaseDBID, "pending", "planning", "race test", "test")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if err == release.ErrReleaseRaceCondition {
				raceErrs++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, wins, "exactly one goroutine must win the transition")
	assert.Equal(t, goroutines-1, raceErrs, "all other goroutines must get race condition error")

	// Verify exactly 1 history row for the pending → planning transition
	rows, err := svc.GetHistory(ctx, releaseDBID)
	require.NoError(t, err)

	planningCount := 0
	for _, row := range rows {
		if row.ToState == "planning" {
			planningCount++
		}
	}
	assert.Equal(t, 1, planningCount, "exactly 1 history row for the planning transition")

	// Verify final status is planning
	updated, err := svc.GetByID(ctx, releaseDBID)
	require.NoError(t, err)
	assert.Equal(t, "planning", updated.Status)
}
