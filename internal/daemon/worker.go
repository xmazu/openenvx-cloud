package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openenvx/cloud/internal/models"
	"github.com/rs/zerolog"
)

type WorkerPool struct {
	logger          zerolog.Logger
	db              JobStore
	infisical       SecretManager
	storage         ObjectStorage
	orchestratorURL string
	systemToken     string
	broker          MessageBroker
	workers         int
	wg              sync.WaitGroup
}

func NewWorkerPool(logger zerolog.Logger, store JobStore, infisical SecretManager, storage ObjectStorage, orchestratorURL string, systemToken string, broker MessageBroker, workers int) *WorkerPool {
	return &WorkerPool{
		logger:          logger.With().Str("component", "worker-pool").Logger(),
		db:              store,
		infisical:       infisical,
		storage:         storage,
		orchestratorURL: orchestratorURL,
		systemToken:     systemToken,
		broker:          broker,
		workers:         workers,
	}
}

func (p *WorkerPool) Start(ctx context.Context) {
	if err := os.RemoveAll("/tmp/openenvx-jobs"); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to clean up orphaned jobs directory")
	}

	p.logger.Info().Int("workers", p.workers).Msg("Starting worker pool")
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			p.worker(ctx, id)
		}(i)
	}
}

func (p *WorkerPool) Stop() {
	p.logger.Info().Msg("Stopping worker pool, waiting for active workers to finish")
	p.wg.Wait()
	p.logger.Info().Msg("Worker pool stopped")
}

func (p *WorkerPool) worker(ctx context.Context, id int) {
	workerLogger := p.logger.With().Int("worker_id", id).Logger()
	workerLogger.Debug().Msg("Worker started")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			workerLogger.Debug().Msg("Worker shutting down")
			return
		case <-ticker.C:
			job, err := p.db.ClaimNextJob(ctx, []models.JobStatus{models.StatusPendingPlan, models.StatusApproved})
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					workerLogger.Error().Err(err).Msg("Failed to claim next job")
				}
				continue
			}
			p.processJob(ctx, job, workerLogger)
		}
	}
}

func (p *WorkerPool) processJob(ctx context.Context, job *models.Job, logger zerolog.Logger) {
	logger = logger.With().Str("job_id", job.ID).Str("operation", job.Operation).Logger()
	logger.Info().Msg("Processing job")
	defer func() {
		promoteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_, err := p.db.PromoteNextJob(promoteCtx, job.ProjectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				logger.Debug().Msg("No queued jobs to promote")
				return
			}

			logger.Error().Err(err).Msg("Failed to promote next job")
			return
		}
	}()

	var logBuffer bytes.Buffer
	err := p.executeJob(ctx, job, &logBuffer, logger)

	if logData := logBuffer.Bytes(); len(logData) > 0 {
		objectName := fmt.Sprintf("jobs/%s/run.log", job.ID)
		_, uploadErr := p.storage.Upload(ctx, objectName, bytes.NewReader(logData), int64(len(logData)), "text/plain")
		if uploadErr != nil {
			logger.Error().Err(uploadErr).Msg("Failed to upload run logs to object storage")
		}

		summary := p.extractSummary(logData)
		if updateErr := p.db.UpdateJobSummary(ctx, job.ID, summary); updateErr != nil {
			logger.Error().Err(updateErr).Msg("Failed to update job summary in database")
		}
	}

	if err != nil {
		logger.Error().Err(err).Msg("Job execution failed")
		if err := p.db.UpdateJobStatus(ctx, job.ID, models.StatusFailed); err != nil {
			logger.Error().Err(err).Msg("Failed to update job status to failed")
		}
		return
	}

	logger.Info().Msg("Job completed successfully")
}

func (p *WorkerPool) extractSummary(logData []byte) string {
	lines := bytes.Split(logData, []byte("\n"))
	start := len(lines) - 50
	if start < 0 {
		start = 0
	}
	summaryLines := lines[start:]
	summary := string(bytes.Join(summaryLines, []byte("\n")))

	if len(summary) > 4000 {
		summary = summary[len(summary)-4000:]
	}
	return summary
}
