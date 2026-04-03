package daemon

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/openenvx/cloud/internal/models"
	"github.com/rs/zerolog"
)

type Daemon struct {
	store           JobStore
	infisical       SecretManager
	storage         ObjectStorage
	broker          MessageBroker
	workerPool      *WorkerPool
	queueTimeout    time.Duration
	orchestratorURL string
	systemToken     string
	logger          zerolog.Logger
	workerPoolSize  int
}

type Option func(*Daemon)

func WithWorkerPoolSize(size int) Option {
	return func(d *Daemon) {
		d.workerPoolSize = size
	}
}

func WithQueueTimeout(timeout time.Duration) Option {
	return func(d *Daemon) {
		d.queueTimeout = timeout
	}
}

func NewDaemon(store JobStore, infisical SecretManager, storage ObjectStorage, orchestratorURL string, systemToken string, broker MessageBroker, logger zerolog.Logger, opts ...Option) *Daemon {
	d := &Daemon{
		store:           store,
		infisical:       infisical,
		storage:         storage,
		orchestratorURL: orchestratorURL,
		systemToken:     systemToken,
		broker:          broker,
		logger:          logger,
		workerPoolSize:  5,
		queueTimeout:    1 * time.Hour,
	}

	for _, opt := range opts {
		opt(d)
	}

	if envTimeout := os.Getenv("QUEUE_TIMEOUT"); envTimeout != "" {
		if t, err := time.ParseDuration(envTimeout); err == nil {
			d.queueTimeout = t
		}
	}

	d.workerPool = NewWorkerPool(d.logger, d.store, d.infisical, d.storage, d.orchestratorURL, d.systemToken, d.broker, d.workerPoolSize)

	return d
}

func (d *Daemon) Start(ctx context.Context) error {
	d.logger.Info().Msg("Starting orchestrator daemon")

	if err := os.MkdirAll("/tmp/openenvx-tf-cache", 0755); err != nil {
		return fmt.Errorf("create terraform plugin cache dir: %w", err)
	}
	d.logger.Info().Msg("Terraform plugin cache directory initialized at /tmp/openenvx-tf-cache")

	d.workerPool.Start(ctx)
	defer d.workerPool.Stop()

	if err := d.recoverJobs(ctx); err != nil {
		d.logger.Error().Err(err).Msg("Error during job recovery")
	}

	cleanupTicker := time.NewTicker(1 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-cleanupTicker.C:
			affected, err := d.store.FailTimedOutJobs(ctx, d.queueTimeout)
			if err != nil {
				d.logger.Error().Err(err).Msg("Error failing timed out jobs")
			} else if affected > 0 {
				d.logger.Info().Int64("count", affected).Msg("Failed timed out queued jobs")
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
