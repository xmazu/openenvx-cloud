package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/terraform"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

func (p *WorkerPool) executeJob(ctx context.Context, job *models.Job, logBuffer io.Writer, logger zerolog.Logger) error {
	if err := p.storage.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("ensure bucket: %w", err)
	}
	secretsPath := fmt.Sprintf("/projects/%s/terraform", job.ProjectID)

	secrets, err := p.infisical.GetSecrets(ctx, job.ProjectID, "prod", secretsPath)
	if err != nil {
		return fmt.Errorf("fetch secrets: %w", err)
	}
	workDir := filepath.Join("/tmp/openenvx-jobs", job.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("mkdir workdir: %w", err)
	}
	defer os.RemoveAll(workDir)
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

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer pr.Close()
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			p.broker.Publish(job.ID, scanner.Text())
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			default:
			}
		}
		return scanner.Err()
	})

	var executeErr error
	g.Go(func() error {
		defer pw.Close()
		backendConfig := terraform.BackendConfig{
			URL:      fmt.Sprintf("%s/internal/api/v1/projects/%s/state", p.orchestratorURL, job.ProjectID),
			Username: "system",
			Password: p.systemToken,
		}

		if err := runner.WriteBackendConfig(backendConfig); err != nil {
			return fmt.Errorf("write backend config: %w", err)
		}
		if err := p.generateTerraformFiles(job, workDir); err != nil {
			return fmt.Errorf("generate terraform files: %w", err)
		}
		_, initStderr, err := runner.Init(gCtx)
		if err != nil {
			return fmt.Errorf("terraform init failed: %w\nstderr: %s", err, string(initStderr))
		}

		switch job.Operation {
		case "plan":
			executeErr = p.handlePlan(gCtx, job, runner, workDir, multiWriter, logger)
		case "apply":
			executeErr = p.handleApply(gCtx, job, runner, workDir, multiWriter, logger)
		case "destroy":
			executeErr = p.handleDestroy(gCtx, job, runner, workDir, multiWriter, logger)
		default:
			executeErr = fmt.Errorf("unknown operation: %s", job.Operation)
		}
		return executeErr
	})

	if err := g.Wait(); err != nil && executeErr == nil {
		return err
	}
	return executeErr
}

func (p *WorkerPool) generateTerraformFiles(job *models.Job, workDir string) error {
	mainTF := fmt.Sprintf(`
module "main" {
  source = "%s"
}
`, job.ModuleName)

	if err := os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0644); err != nil {
		return fmt.Errorf("write main.tf: %w", err)
	}
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
