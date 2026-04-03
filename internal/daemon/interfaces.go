package daemon

import (
	"context"
	"io"
	"time"

	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/storage"
)

type JobStore interface {
	FetchJobsByStatuses(ctx context.Context, statuses []models.JobStatus) ([]*models.Job, error)
	UpdateJobStatus(ctx context.Context, id string, status models.JobStatus) error
	GetJob(ctx context.Context, id string) (*models.Job, error)
	FailTimedOutJobs(ctx context.Context, timeout time.Duration) (int64, error)
	ClaimNextJob(ctx context.Context, statuses []models.JobStatus) (*models.Job, error)
	UpdateJobPlanResult(ctx context.Context, id string, planOutputPath string, planSummary string) error
	UpdateJobSummary(ctx context.Context, id string, summary string) error
	PromoteNextJob(ctx context.Context, projectID string) (*models.Job, error)
}

type ObjectStorage interface {
	Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error)
	Download(ctx context.Context, objectName string) (io.ReadCloser, error)
	Stat(ctx context.Context, objectName string) (storage.ObjectInfo, error)
	EnsureBucket(ctx context.Context) error
}

type SecretManager interface {
	GetSecrets(ctx context.Context, projectID, env, path string) (map[string]string, error)
}

type MessageBroker interface {
	Publish(topic string, message string)
}
