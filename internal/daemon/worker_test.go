package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/terraform"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorker_MidFlightCancellation_Plan(t *testing.T) {
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

	schemaName := "test_cancellation_" + time.Now().Format("20060102150405")
	_, err = pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schemaName)
	require.NoError(t, err)
	defer pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE")

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaName)
		return err
	}
	pool.Close()
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

	store := db.NewStore(pool)

	job, err := store.CreateJob(ctx, "project-123", "plan", "test-module", map[string]interface{}{})
	require.NoError(t, err)

	// Create temporary workdir with a Terraform file that takes a few seconds to plan
	workDir := t.TempDir()
	tfContent := `
data "external" "sleep" {
  program = ["sh", "-c", "sleep 3; echo '{\"status\": \"ok\"}'"]
}
`
	err = os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(tfContent), 0644)
	require.NoError(t, err)

	runner, err := terraform.NewRunner(workDir, nil)
	require.NoError(t, err)

	_, _, err = runner.Init(ctx)
	require.NoError(t, err)

	logger := zerolog.Nop()
	wp := &WorkerPool{
		logger: logger,
		db:     store,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		err := wp.handlePlan(ctx, job, runner, workDir, logger)
		assert.NoError(t, err)
	}()

	// Wait 1 second to ensure plan is running, then cancel job in DB
	time.Sleep(1 * time.Second)
	err = store.UpdateJobStatus(ctx, job.ID, models.StatusCancelled)
	require.NoError(t, err)

	wg.Wait()

	// Verify that the job status was not updated to PLANNED
	finalJob, err := store.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelled, finalJob.Status)
}

func TestWorker_MidFlightCancellation_Apply(t *testing.T) {
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

	schemaName := "test_cancellation_apply_" + time.Now().Format("20060102150405")
	_, err = pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schemaName)
	require.NoError(t, err)
	defer pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE")

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaName)
		return err
	}
	pool.Close()
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

	store := db.NewStore(pool)

	job, err := store.CreateJob(ctx, "project-123", "apply", "test-module", map[string]interface{}{})
	require.NoError(t, err)

	// Create temporary workdir with a Terraform file that takes a few seconds to apply
	workDir := t.TempDir()
	tfContent := `
resource "time_sleep" "wait" {
  create_duration = "3s"
}
`
	err = os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(tfContent), 0644)
	require.NoError(t, err)

	runner, err := terraform.NewRunner(workDir, nil)
	require.NoError(t, err)

	_, _, err = runner.Init(ctx)
	require.NoError(t, err)

	logger := zerolog.Nop()
	wp := &WorkerPool{
		logger: logger,
		db:     store,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		err := wp.handleApply(ctx, job, runner, logger)
		assert.NoError(t, err)
	}()

	// Wait 1 second to ensure apply is running, then cancel job in DB
	time.Sleep(1 * time.Second)
	err = store.UpdateJobStatus(ctx, job.ID, models.StatusCancelled)
	require.NoError(t, err)

	wg.Wait()

	// Verify that the job status was not updated to APPLIED
	finalJob, err := store.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelled, finalJob.Status)
}
