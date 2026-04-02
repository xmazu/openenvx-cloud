package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/infisical"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/pubsub"
	"github.com/openenvx/cloud/internal/storage"
	"github.com/rs/zerolog"
)

type Daemon struct {
	store           *db.Store
	workerPool      *WorkerPool
	pollInterval    time.Duration
	orchestratorURL string
	logger          *zerolog.Logger
	mu              sync.RWMutex
	activeJobs      map[string]struct{}
}

func NewDaemon(store *db.Store, infisical *infisical.Client, storage *storage.Storage, orchestratorURL string, broker *pubsub.Broker, workerPoolSize int, pollInterval time.Duration, logger *zerolog.Logger) *Daemon {
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	if workerPoolSize == 0 {
		workerPoolSize = 5
	}

	workerPool := NewWorkerPool(*logger, store, infisical, storage, orchestratorURL, broker, workerPoolSize)

	return &Daemon{
		store:           store,
		workerPool:      workerPool,
		pollInterval:    pollInterval,
		orchestratorURL: orchestratorURL,
		logger:          logger,
		activeJobs:      make(map[string]struct{}),
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	d.logger.Info().Msgf("Starting orchestrator daemon, polling every %v", d.pollInterval)

	if err := os.MkdirAll("/tmp/openenvx-tf-cache", 0755); err != nil {
		return fmt.Errorf("create terraform plugin cache dir: %w", err)
	}
	d.logger.Info().Msg("Terraform plugin cache directory initialized at /tmp/openenvx-tf-cache")

	d.workerPool.Start(ctx)

	if err := d.recoverJobs(ctx); err != nil {
		d.logger.Error().Err(err).Msg("Error during job recovery")
	}

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := d.processJobs(ctx); err != nil {
				d.logger.Error().Err(err).Msg("Error processing jobs")
			}
		}
	}
}

func (d *Daemon) recoverJobs(ctx context.Context) error {
	d.logger.Info().Msg("Performing job recovery")

	staleStatuses := []models.JobStatus{
		models.StatusPlanning,
		models.StatusApplying,
		models.StatusDestroying,
	}

	jobs, err := d.store.FetchJobsByStatuses(ctx, staleStatuses)
	if err != nil {
		return fmt.Errorf("fetch stale jobs: %w", err)
	}

	for _, job := range jobs {
		d.logger.Warn().
			Str("job_id", job.ID).
			Str("status", string(job.Status)).
			Msg("Stale job found on startup, marking as FAILED")

		if err := d.store.UpdateJobStatus(ctx, job.ID, models.StatusFailed); err != nil {
			d.logger.Error().
				Err(err).
				Str("job_id", job.ID).
				Msg("Failed to update stale job status")
		}
	}

	return nil
}

func (d *Daemon) processJobs(ctx context.Context) error {
	pendingStatuses := []models.JobStatus{
		models.StatusPendingPlan,
		models.StatusApproved,
	}

	jobs, err := d.store.FetchJobsByStatuses(ctx, pendingStatuses)
	if err != nil {
		return fmt.Errorf("fetch pending jobs: %w", err)
	}

	for _, job := range jobs {
		if !d.isJobActive(job.ID) {
			d.logger.Info().Str("id", job.ID).Str("status", string(job.Status)).Msg("Submitting job to worker pool")
			d.markJobActive(job.ID)

			go func(j *models.Job) {
				d.workerPool.Submit(j)
			}(job)
		}
	}

	d.cleanupActiveJobs(ctx)

	return nil
}

func (d *Daemon) isJobActive(id string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.activeJobs[id]
	return ok
}

func (d *Daemon) markJobActive(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeJobs[id] = struct{}{}
}

func (d *Daemon) cleanupActiveJobs(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for id := range d.activeJobs {
		job, err := d.store.GetJob(ctx, id)
		if err != nil {
			d.logger.Error().Err(err).Str("id", id).Msg("Failed to check job status for cleanup")
			continue
		}

		if job.Status != models.StatusPendingPlan && job.Status != models.StatusApproved {
			delete(d.activeJobs, id)
		}
	}
}
