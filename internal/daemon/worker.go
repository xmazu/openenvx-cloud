package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/infisical"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/storage"
	"github.com/openenvx/cloud/internal/terraform"
	"github.com/rs/zerolog"
)

type WorkerPool struct {
	logger    zerolog.Logger
	db        *db.Store
	infisical *infisical.Client
	storage   *storage.Storage
	jobChan   chan *models.Job
	workers   int
}

func NewWorkerPool(logger zerolog.Logger, store *db.Store, infisical *infisical.Client, storage *storage.Storage, workers int) *WorkerPool {
	return &WorkerPool{
		logger:    logger.With().Str("component", "worker-pool").Logger(),
		db:        store,
		infisical: infisical,
		storage:   storage,
		jobChan:   make(chan *models.Job, 100), // Buffer size for incoming jobs
		workers:   workers,
	}
}

func (p *WorkerPool) Start(ctx context.Context) {
	p.logger.Info().Int("workers", p.workers).Msg("Starting worker pool")
	for i := 0; i < p.workers; i++ {
		go p.worker(ctx, i)
	}
}

func (p *WorkerPool) Submit(job *models.Job) {
	p.jobChan <- job
}

func (p *WorkerPool) worker(ctx context.Context, id int) {
	workerLogger := p.logger.With().Int("worker_id", id).Logger()
	workerLogger.Debug().Msg("Worker started")

	for {
		select {
		case <-ctx.Done():
			workerLogger.Debug().Msg("Worker shutting down")
			return
		case job := <-p.jobChan:
			p.processJob(ctx, job, workerLogger)
		}
	}
}

func (p *WorkerPool) processJob(ctx context.Context, job *models.Job, logger zerolog.Logger) {
	logger = logger.With().Str("job_id", job.ID).Str("operation", job.Operation).Logger()
	logger.Info().Msg("Processing job")

	// Update status based on operation
	var startStatus models.JobStatus
	if job.Operation == "plan" {
		startStatus = models.StatusPlanning
	} else if job.Operation == "apply" {
		startStatus = models.StatusApplying
	}

	if startStatus != "" {
		if err := p.db.UpdateJobStatus(ctx, job.ID, startStatus); err != nil {
			logger.Error().Err(err).Msg("Failed to update job status to starting status")
			return
		}
	}

	if err := p.executeJob(ctx, job, logger); err != nil {
		logger.Error().Err(err).Msg("Job execution failed")
		if err := p.db.UpdateJobStatus(ctx, job.ID, models.StatusFailed); err != nil {
			logger.Error().Err(err).Msg("Failed to update job status to failed")
		}
		return
	}

	logger.Info().Msg("Job completed successfully")
}

func (p *WorkerPool) executeJob(ctx context.Context, job *models.Job, logger zerolog.Logger) error {
	// 1. Ensure Minio bucket
	if err := p.storage.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("ensure bucket: %w", err)
	}

	// 2. Fetch secrets from Infisical
	secretsPath := fmt.Sprintf("/projects/%s/terraform", job.ProjectID)
	secrets, err := p.infisical.GetSecrets(job.ProjectID, "prod", secretsPath)
	if err != nil {
		return fmt.Errorf("fetch secrets: %w", err)
	}

	// 3. Create unique working directory
	workDir := filepath.Join("/tmp/openenvx-jobs", job.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("mkdir workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 4. Initialize Terraform runner
	runner, err := terraform.NewRunner(workDir, secrets)
	if err != nil {
		return fmt.Errorf("init terraform runner: %w", err)
	}

	// 5. Run Init()
	_, initStderr, err := runner.Init(ctx)
	if err != nil {
		return fmt.Errorf("terraform init failed: %w\nstderr: %s", err, string(initStderr))
	}

	// 6. Handle operation
	if job.Operation == "plan" {
		return p.handlePlan(ctx, job, runner, workDir, logger)
	} else if job.Operation == "apply" {
		return p.handleApply(ctx, job, runner, logger)
	}

	return fmt.Errorf("unknown operation: %s", job.Operation)
}

func (p *WorkerPool) handlePlan(ctx context.Context, job *models.Job, runner *terraform.Runner, workDir string, logger zerolog.Logger) error {
	planFilename := "tfplan"
	planPath := filepath.Join(workDir, planFilename)

	_, planStderr, err := runner.Plan(ctx, planPath)
	if err != nil {
		return fmt.Errorf("terraform plan failed: %w\nstderr: %s", err, string(planStderr))
	}

	currentJob, err := p.db.GetJob(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch job status after plan: %w", err)
	}
	if currentJob.Status == models.StatusCancelled {
		logger.Warn().Msg("job was cancelled mid-flight, skipping updates")
		return nil
	}

	showStdout, showStderr, err := runner.Show(ctx, planPath)
	if err != nil {
		return fmt.Errorf("terraform show failed: %w\nstderr: %s", err, string(showStderr))
	}

	planSummary := string(showStdout)

	planData, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("read plan file: %w", err)
	}

	objectName := fmt.Sprintf("jobs/%s/tfplan", job.ID)
	_, err = p.storage.Upload(ctx, objectName, bytes.NewReader(planData), int64(len(planData)), "application/octet-stream")
	if err != nil {
		return fmt.Errorf("upload plan: %w", err)
	}

	if err := p.db.UpdateJobPlanResult(ctx, job.ID, objectName, planSummary); err != nil {
		return fmt.Errorf("update job plan result in db: %w", err)
	}

	if err := p.db.UpdateJobStatus(ctx, job.ID, models.StatusPlanned); err != nil {
		return fmt.Errorf("update job status to planned: %w", err)
	}

	return nil
}

func (p *WorkerPool) handleApply(ctx context.Context, job *models.Job, runner *terraform.Runner, logger zerolog.Logger) error {
	// If it's an apply operation, we might want to fetch the plan from storage first if we were strict,
	// but the original logic just ran apply (which might do an implicit plan or use a local one if it existed).
	// Given the instructions to port exactly, I'll follow the old logic.
	planPath := "" // Old logic used empty string for apply

	_, applyStderr, err := runner.Apply(ctx, planPath)
	if err != nil {
		return fmt.Errorf("terraform apply failed: %w\nstderr: %s", err, string(applyStderr))
	}

	currentJob, err := p.db.GetJob(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch job status after apply: %w", err)
	}
	if currentJob.Status == models.StatusCancelled {
		logger.Warn().Msg("job was cancelled mid-flight, skipping updates")
		return nil
	}

	if err := p.db.UpdateJobStatus(ctx, job.ID, models.StatusApplied); err != nil {
		return fmt.Errorf("update job status to applied: %w", err)
	}

	return nil
}
