package daemon

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/terraform"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

type jobHandler interface {
	Execute(ctx context.Context) error
}

type jobHandlerFunc func(ctx context.Context) error

func (f jobHandlerFunc) Execute(ctx context.Context) error {
	return f(ctx)
}

func (p *WorkerPool) withHooks(job *models.Job, preHooks, postHooks []string, workDir string, logBuffer io.Writer, logger zerolog.Logger, next jobHandler) jobHandler {
	return jobHandlerFunc(func(ctx context.Context) error {
		if err := p.runHooks(ctx, job.ID, preHooks, workDir, logBuffer, logger); err != nil {
			return err
		}
		if err := next.Execute(ctx); err != nil {
			return err
		}
		return p.runHooks(ctx, job.ID, postHooks, workDir, logBuffer, logger)
	})
}

func (p *WorkerPool) withCancellationCheck(jobID string, logger zerolog.Logger, next jobHandler) jobHandler {
	return jobHandlerFunc(func(ctx context.Context) error {
		if err := next.Execute(ctx); err != nil {
			return err
		}
		currentJob, err := p.db.GetJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("failed to fetch job status: %w", err)
		}
		if currentJob.Status == models.StatusCancelled {
			logger.Warn().Msg("job was cancelled mid-flight, skipping updates")
			return nil
		}
		return nil
	})
}

func (p *WorkerPool) handlePlan(ctx context.Context, job *models.Job, runner *terraform.Runner, workDir string, logBuffer io.Writer, logger zerolog.Logger) error {
	planFilename := "tfplan"
	planPath := filepath.Join(workDir, planFilename)

	handler := jobHandlerFunc(func(ctx context.Context) error {
		_, planStderr, err := runner.Plan(ctx, planPath)
		if err != nil {
			return fmt.Errorf("terraform plan failed: %w\nstderr: %s", err, string(planStderr))
		}
		return nil
	})

	pipeline := p.withHooks(job, job.PrePlan, job.PostPlan, workDir, logBuffer, logger,
		p.withCancellationCheck(job.ID, logger, handler))

	if err := pipeline.Execute(ctx); err != nil {
		return err
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
	var rc io.ReadCloser
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

	handler := jobHandlerFunc(func(ctx context.Context) error {
		_, applyStderr, err := runner.Apply(ctx, planPath)
		if err != nil {
			return fmt.Errorf("terraform apply failed: %w\nstderr: %s", err, string(applyStderr))
		}
		return nil
	})

	pipeline := p.withHooks(job, job.PreApply, job.PostApply, workDir, logBuffer, logger,
		p.withCancellationCheck(job.ID, logger, handler))

	if err := pipeline.Execute(ctx); err != nil {
		return err
	}

	if err := p.db.UpdateJobStatus(ctx, job.ID, models.StatusApplied); err != nil {
		return fmt.Errorf("update job status to applied: %w", err)
	}

	return nil
}

func (p *WorkerPool) handleDestroy(ctx context.Context, job *models.Job, runner *terraform.Runner, workDir string, logBuffer io.Writer, logger zerolog.Logger) error {
	planFilename := "tfplan"
	planPath := filepath.Join(workDir, planFilename)

	handler := jobHandlerFunc(func(ctx context.Context) error {
		_, planStderr, err := runner.Plan(ctx, planPath, tfexec.Destroy(true))
		if err != nil {
			return fmt.Errorf("terraform plan -destroy failed: %w\nstderr: %s", err, string(planStderr))
		}
		return nil
	})

	pipeline := p.withHooks(job, job.PreDestroy, nil, workDir, logBuffer, logger,
		p.withCancellationCheck(job.ID, logger, handler))

	if err := pipeline.Execute(ctx); err != nil {
		return err
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
		g, gCtx := errgroup.WithContext(ctx)

		g.Go(func() error {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				multiWriter.Write([]byte(line + "\n"))
				p.broker.Publish(jobID, line)
				select {
				case <-gCtx.Done():
					return gCtx.Err()
				default:
				}
			}
			return scanner.Err()
		})

		waitErr := cmd.Wait()
		scanErr := g.Wait()

		if waitErr != nil {
			return fmt.Errorf("hook %s: failed: %w", hook, waitErr)
		}
		if scanErr != nil && !errors.Is(scanErr, context.Canceled) {
			return fmt.Errorf("hook %s: scan error: %w", hook, scanErr)
		}
	}
	return nil
}
