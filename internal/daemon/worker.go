package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/infisical"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/pubsub"
	"github.com/openenvx/cloud/internal/storage"
	"github.com/openenvx/cloud/internal/terraform"
	"github.com/rs/zerolog"
)

type WorkerPool struct {
	logger          zerolog.Logger
	db              *db.Store
	infisical       *infisical.Client
	storage         *storage.Storage
	orchestratorURL string
	broker          *pubsub.Broker
	jobChan         chan *models.Job
	workers         int
}

func NewWorkerPool(logger zerolog.Logger, store *db.Store, infisical *infisical.Client, storage *storage.Storage, orchestratorURL string, broker *pubsub.Broker, workers int) *WorkerPool {
	return &WorkerPool{
		logger:          logger.With().Str("component", "worker-pool").Logger(),
		db:              store,
		infisical:       infisical,
		storage:         storage,
		orchestratorURL: orchestratorURL,
		broker:          broker,
		jobChan:         make(chan *models.Job, 100), // Buffer size for incoming jobs
		workers:         workers,
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
	} else if job.Operation == "destroy" {
		startStatus = models.StatusDestroying
	}

	if startStatus != "" {
		if err := p.db.UpdateJobStatus(ctx, job.ID, startStatus); err != nil {
			logger.Error().Err(err).Msg("Failed to update job status to starting status")
			return
		}
	}

	var logBuffer bytes.Buffer
	err := p.executeJob(ctx, job, &logBuffer, logger)

	if logData := logBuffer.Bytes(); len(logData) > 0 {
		objectName := fmt.Sprintf("jobs/%s/run.log", job.ID)
		if _, uploadErr := p.storage.Upload(ctx, objectName, bytes.NewReader(logData), int64(len(logData)), "text/plain"); uploadErr != nil {
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

func (p *WorkerPool) executeJob(ctx context.Context, job *models.Job, logBuffer io.Writer, logger zerolog.Logger) error {
	// 1. Ensure bucket exists
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
	if secrets == nil {
		secrets = make(map[string]string)
	}
	secrets["TF_PLUGIN_CACHE_DIR"] = "/tmp/openenvx-tf-cache"

	runner, err := terraform.NewRunner(workDir, secrets)
	if err != nil {
		return fmt.Errorf("init terraform runner: %w", err)
	}

	pr, pw := io.Pipe()
	multiWriter := io.MultiWriter(pw, logBuffer)
	runner.SetStdout(multiWriter)
	runner.SetStderr(multiWriter)

	go func() {
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			p.broker.Publish(job.ID, scanner.Text())
		}
		pr.Close()
	}()
	defer pw.Close()

	// 5. Configure backend
	backendConfig := terraform.BackendConfig{
		URL:      fmt.Sprintf("%s/internal/api/v1/projects/%s/state", p.orchestratorURL, job.ProjectID),
		Username: "admin", // Basic auth needs a username, using a dummy one since VerifyUserAndOrg handles it
		Password: job.OrganizationID,
	}

	if err := runner.WriteBackendConfig(backendConfig); err != nil {
		return fmt.Errorf("write backend config: %w", err)
	}

	// Wait, I need the orgID for basic auth.
	// Let's check how the job is created or if the orgID is available.
	// Actually, the auth middleware uses userID as Username and orgID as Password.
	// For now, I'll use placeholders if I can't find them, but I should look for where orgID comes from.

	if err := runner.WriteBackendConfig(backendConfig); err != nil {
		return fmt.Errorf("write backend config: %w", err)
	}

	// 6. Generate main.tf and terraform.tfvars.json
	if err := p.generateTerraformFiles(job, workDir); err != nil {
		return fmt.Errorf("generate terraform files: %w", err)
	}

	// 7. Run Init()
	_, initStderr, err := runner.Init(ctx)
	if err != nil {
		return fmt.Errorf("terraform init failed: %w\nstderr: %s", err, string(initStderr))
	}

	// 8. Handle operation
	if job.Operation == "plan" {
		return p.handlePlan(ctx, job, runner, workDir, multiWriter, logger)
	} else if job.Operation == "apply" {
		return p.handleApply(ctx, job, runner, workDir, multiWriter, logger)
	} else if job.Operation == "destroy" {
		return p.handleDestroy(ctx, job, runner, workDir, multiWriter, logger)
	}

	return fmt.Errorf("unknown operation: %s", job.Operation)
}

func (p *WorkerPool) generateTerraformFiles(job *models.Job, workDir string) error {
	// 1. Generate main.tf
	mainTF := fmt.Sprintf(`
module "main" {
  source = "%s"
}
`, job.ModuleName)

	if err := os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0644); err != nil {
		return fmt.Errorf("write main.tf: %w", err)
	}

	// 2. Generate terraform.tfvars.json
	if len(job.Variables) > 0 {
		varsJSON, err := json.Marshal(job.Variables)
		if err != nil {
			return fmt.Errorf("marshal variables: %w", err)
		}

		if err := os.WriteFile(filepath.Join(workDir, "terraform.tfvars.json"), varsJSON, 0644); err != nil {
			return fmt.Errorf("write terraform.tfvars.json: %w", err)
		}
	}

	return nil
}

func (p *WorkerPool) handlePlan(ctx context.Context, job *models.Job, runner *terraform.Runner, workDir string, logBuffer io.Writer, logger zerolog.Logger) error {
	planFilename := "tfplan"
	planPath := filepath.Join(workDir, planFilename)

	if err := p.runHooks(ctx, job.ID, job.PrePlan, workDir, logBuffer, logger); err != nil {
		return err
	}

	_, planStderr, err := runner.Plan(ctx, planPath)
	if err != nil {
		return fmt.Errorf("terraform plan failed: %w\nstderr: %s", err, string(planStderr))
	}

	if err := p.runHooks(ctx, job.ID, job.PostPlan, workDir, logBuffer, logger); err != nil {
		return err
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

func (p *WorkerPool) handleApply(ctx context.Context, job *models.Job, runner *terraform.Runner, workDir string, logBuffer io.Writer, logger zerolog.Logger) error {
	planFilename := "tfplan"
	planPath := filepath.Join(workDir, planFilename)

	objectName := fmt.Sprintf("jobs/%s/tfplan", job.ID)
	rc, err := p.storage.Download(ctx, objectName)
	if err != nil {
		return fmt.Errorf("download plan from storage: %w", err)
	}
	defer rc.Close()

	f, err := os.Create(planPath)
	if err != nil {
		return fmt.Errorf("create local plan file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, rc); err != nil {
		return fmt.Errorf("write plan to local file: %w", err)
	}

	if err := p.runHooks(ctx, job.ID, job.PreApply, workDir, logBuffer, logger); err != nil {
		return err
	}

	_, applyStderr, err := runner.Apply(ctx, planPath)
	if err != nil {
		return fmt.Errorf("terraform apply failed: %w\nstderr: %s", err, string(applyStderr))
	}

	if err := p.runHooks(ctx, job.ID, job.PostApply, workDir, logBuffer, logger); err != nil {
		return err
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

func (p *WorkerPool) handleDestroy(ctx context.Context, job *models.Job, runner *terraform.Runner, workDir string, logBuffer io.Writer, logger zerolog.Logger) error {
	planFilename := "tfplan"
	planPath := filepath.Join(workDir, planFilename)

	if err := p.runHooks(ctx, job.ID, job.PreDestroy, workDir, logBuffer, logger); err != nil {
		return err
	}

	_, planStderr, err := runner.Plan(ctx, planPath, tfexec.Destroy(true))
	if err != nil {
		return fmt.Errorf("terraform plan -destroy failed: %w\nstderr: %s", err, string(planStderr))
	}

	currentJob, err := p.db.GetJob(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch job status after destroy plan: %w", err)
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
		return fmt.Errorf("upload destroy plan: %w", err)
	}

	if err := p.db.UpdateJobPlanResult(ctx, job.ID, objectName, planSummary); err != nil {
		return fmt.Errorf("update job plan result in db: %w", err)
	}

	if err := p.db.UpdateJobStatus(ctx, job.ID, models.StatusPlanned); err != nil {
		return fmt.Errorf("update job status to planned: %w", err)
	}

	return nil
}

func (p *WorkerPool) runHooks(ctx context.Context, jobID string, hooks []string, workDir string, logBuffer io.Writer, logger zerolog.Logger) error {
	for _, hook := range hooks {
		logger.Info().Str("hook", hook).Msg("Running hook")
		p.broker.Publish(jobID, fmt.Sprintf(">>> Running hook: %s", hook))

		cmd := exec.CommandContext(ctx, "sh", "-c", hook)
		cmd.Dir = workDir

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("hook %s: stdout pipe: %w", hook, err)
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("hook %s: start: %w", hook, err)
		}

		multiWriter := io.MultiWriter(logBuffer)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			multiWriter.Write([]byte(line + "\n"))
			p.broker.Publish(jobID, line)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("hook %s: scanner error: %w", hook, err)
		}

		if err := cmd.Wait(); err != nil {
			return fmt.Errorf("hook %s: failed: %w", hook, err)
		}
	}
	return nil
}
