package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/pubsub"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (*db.Store, func()) {
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

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Database ping failed: %v", err)
	}

	schemaName := "test_api_locking_" + time.Now().Format("20060102150405")
	_, err = pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schemaName)
	require.NoError(t, err)

	pool.Close()

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaName)
		return err
	}
	pool, err = pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS member (
			user_id VARCHAR(255) NOT NULL,
			organization_id VARCHAR(255) NOT NULL,
			PRIMARY KEY (user_id, organization_id)
		);
		CREATE TABLE IF NOT EXISTS jobs (
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

	// Seed test user
	_, err = pool.Exec(ctx, "INSERT INTO member (user_id, organization_id) VALUES ($1, $2)", "test-user", "test-org")
	require.NoError(t, err)

	store := db.NewStore(pool)
	cleanup := func() {
		// Use a fresh connection for cleanup
		cleanupPool, _ := pgxpool.New(context.Background(), dbURL)
		if cleanupPool != nil {
			cleanupPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE")
			cleanupPool.Close()
		}
		pool.Close()
	}

	return store, cleanup
}

func TestCreateJob_Conflict(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	logger := zerolog.New(nil)
	broker := pubsub.NewBroker()
	s := NewServer(store, nil, &logger, broker)
	handler := s.Routes()

	projectID := "proj-1"
	auth := "test-user:test-org"
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))

	// 1. Create first job
	reqBody := createJobRequest{
		ProjectID:  projectID,
		Operation:  "plan",
		ModuleName: "mod",
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/internal/api/v1/jobs", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var firstJob models.Job
	err := json.Unmarshal(rec.Body.Bytes(), &firstJob)
	require.NoError(t, err)

	// 2. Try creating second job (should conflict)
	req = httptest.NewRequest(http.MethodPost, "/internal/api/v1/jobs", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
	var resp map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Project is locked", resp["error"])
	assert.Equal(t, firstJob.ID, resp["locked_by_job_id"])
}

func TestDiscardJob(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	logger := zerolog.New(nil)
	broker := pubsub.NewBroker()
	s := NewServer(store, nil, &logger, broker)
	handler := s.Routes()

	projectID := "proj-1"
	auth := "test-user:test-org"
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))

	// 1. Create job
	reqBody := createJobRequest{
		ProjectID:  projectID,
		Operation:  "plan",
		ModuleName: "mod",
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/internal/api/v1/jobs", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var job models.Job
	err := json.Unmarshal(rec.Body.Bytes(), &job)
	require.NoError(t, err)

	// 2. Discard job
	req = httptest.NewRequest(http.MethodPost, "/internal/api/v1/jobs/"+job.ID+"/discard", nil)
	req.Header.Set("Authorization", authHeader)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 3. Verify status is CANCELLED
	dbJob, err := store.GetJob(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCancelled, dbJob.Status)

	// 4. Try discarding again (should fail with 400 because it's already CANCELLED)
	req = httptest.NewRequest(http.MethodPost, "/internal/api/v1/jobs/"+job.ID+"/discard", nil)
	req.Header.Set("Authorization", authHeader)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
