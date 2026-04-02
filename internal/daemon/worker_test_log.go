package daemon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/db"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorker_LogPersistenceAndSummary(t *testing.T) {
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

	schemaName := "test_logs_" + time.Now().Format("20060102150405")
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
		CREATE TABLE projects (
			id VARCHAR(255) PRIMARY KEY,
			organization_id VARCHAR(255) NOT NULL
		);
	`
	_, err = pool.Exec(ctx, createTableSQL)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "INSERT INTO projects (id, organization_id) VALUES ('project-123', 'org-123')")
	require.NoError(t, err)

	store := db.NewStore(pool)
	job, err := store.CreateJob(ctx, "project-123", "plan", "test-module", map[string]interface{}{}, nil, nil, nil, nil, nil)
	require.NoError(t, err)

	logger := zerolog.Nop()
	wp := &WorkerPool{
		logger: logger,
		db:     store,
	}

	logData := []byte("line 1\nline 2\nline 3\n")
	summary := wp.extractSummary(logData)
	assert.Contains(t, summary, "line 3")

	err = store.UpdateJobSummary(ctx, job.ID, summary)
	require.NoError(t, err)

	finalJob, err := store.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.NotNil(t, finalJob.PlanSummary)
	assert.Equal(t, summary, *finalJob.PlanSummary)
}
