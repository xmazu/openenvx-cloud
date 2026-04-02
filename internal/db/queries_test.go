package db

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateJob_Concurrent(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/openenvx?sslmode=disable"
	}

	ctx := context.Background()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("Database not available for testing: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Database ping failed: %v", err)
	}

	schemaName := "test_concurrent_jobs_" + time.Now().Format("20060102150405")
	_, err = pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schemaName)
	require.NoError(t, err)
	defer pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE")

	pool.Close()

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaName)
		return err
	}
	pool, err = pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	defer pool.Close()

	createTableSQL := `
		CREATE TABLE jobs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			project_id VARCHAR(255) NOT NULL,
			status VARCHAR(50) NOT NULL,
			operation VARCHAR(50) NOT NULL,
			module_name VARCHAR(255) NOT NULL,
			variables JSONB NOT NULL DEFAULT '{}',
			plan_output_path TEXT,
			plan_summary TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);
	`
	_, err = pool.Exec(ctx, createTableSQL)
	require.NoError(t, err)

	store := NewStore(pool)

	projectID := "test-project-123"
	operation := "plan"
	moduleName := "test-module"
	variables := map[string]interface{}{"foo": "bar"}

	numWorkers := 10
	var wg sync.WaitGroup
	results := make(chan error, numWorkers)

	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			_, err := store.CreateJob(ctx, projectID, operation, moduleName, variables)
			results <- err
		}()
	}

	wg.Wait()
	close(results)

	successCount := 0
	conflictCount := 0

	for err := range results {
		if err == nil {
			successCount++
		} else if errors.Is(err, pgx.ErrNoRows) {
			conflictCount++
		} else {
			t.Errorf("Unexpected error: %v", err)
		}
	}

	assert.Equal(t, 1, successCount, "Exactly one CreateJob should succeed")
	assert.Equal(t, numWorkers-1, conflictCount, "All other CreateJob calls should fail with pgx.ErrNoRows")

	activeJob, err := store.GetActiveJobForProject(ctx, projectID)
	require.NoError(t, err)
	assert.NotNil(t, activeJob)
	assert.Equal(t, projectID, activeJob.ProjectID)
	assert.Equal(t, models.StatusPendingPlan, activeJob.Status)
}
