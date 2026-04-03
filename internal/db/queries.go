package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/models"
)

type JobStore interface {
	FetchJobsByStatus(ctx context.Context, status models.JobStatus) ([]*models.Job, error)
	FetchJobsByStatuses(ctx context.Context, statuses []models.JobStatus) ([]*models.Job, error)
	UpdateJobStatus(ctx context.Context, id string, status models.JobStatus) error
	UpdateJobPlanResult(ctx context.Context, id string, planOutputPath string, planSummary string) error
	UpdateJobSummary(ctx context.Context, id string, summary string) error
	GetJob(ctx context.Context, id string) (*models.Job, error)
	CreateJob(ctx context.Context, projectID string, status models.JobStatus, operation string, moduleName string, variables map[string]interface{}, prePlan, postPlan, preApply, postApply, preDestroy []string) (*models.Job, error)
	GetActiveJobForProject(ctx context.Context, projectID string) (*models.Job, error)
	IsProjectActive(ctx context.Context, projectID string) (bool, error)
	PromoteNextJob(ctx context.Context, projectID string) (*models.Job, error)
	FailTimedOutJobs(ctx context.Context, timeout time.Duration) (int64, error)
	ClaimNextJob(ctx context.Context, statuses []models.JobStatus) (*models.Job, error)
	VerifyUserAndOrg(ctx context.Context, userID, orgID string) (bool, error)
	UnlockProjectState(ctx context.Context, projectID string, lockID string) error
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
