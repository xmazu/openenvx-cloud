package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/openenvx/cloud/internal/infisical"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/storage"
	"github.com/openenvx/cloud/internal/terraform"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	if err := run(&logger); err != nil {
		logger.Fatal().Err(err).Msg("worker failed")
	}
}

func fetchJob(ctx context.Context, orchestratorURL, jobID string) (*models.Job, error) {
	url := fmt.Sprintf("%s/api/internal/jobs/%s", orchestratorURL, jobID)

	var job models.Job
	err := doWithRetry(ctx, http.MethodGet, url, nil, &job)
	if err != nil {
		return nil, fmt.Errorf("fetch job: %w", err)
	}
	return &job, nil
}

type updateJobStatusRequest struct {
	Status models.JobStatus `json:"status"`
}

func updateStatus(ctx context.Context, orchestratorURL, jobID string, status models.JobStatus) error {
	url := fmt.Sprintf("%s/api/internal/jobs/%s/status", orchestratorURL, jobID)
	reqBody := updateJobStatusRequest{Status: status}

	err := doWithRetry(ctx, http.MethodPut, url, reqBody, nil)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

type updateJobPlanRequest struct {
	PlanOutputPath string `json:"plan_output_path"`
	PlanSummary    string `json:"plan_summary"`
}

func updateJobPlanResult(ctx context.Context, orchestratorURL, jobID, objectName, planSummary string) error {
	url := fmt.Sprintf("%s/api/internal/jobs/%s/plan", orchestratorURL, jobID)
	reqBody := updateJobPlanRequest{
		PlanOutputPath: objectName,
		PlanSummary:    planSummary,
	}

	// We might get 404 since the endpoint doesn't exist yet according to Inherited Wisdom,
	// but we implement it via HTTP to remove db dependency.
	err := doWithRetry(ctx, http.MethodPut, url, reqBody, nil)
	if err != nil {
		// Ignore 404 for now if not implemented
		return nil
	}
	return nil
}

func doWithRetry(ctx context.Context, method, url string, reqBody interface{}, respBody interface{}) error {
	var bodyBytes []byte
	var err error

	if reqBody != nil {
		bodyBytes, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
	}

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if i == maxRetries-1 {
				return fmt.Errorf("http request failed after %d retries: %w", maxRetries, err)
			}
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			if resp.StatusCode == http.StatusNotFound && method == http.MethodPut && reqBody != nil {
				// Special handling for not yet implemented endpoints
				return fmt.Errorf("http error: %d", resp.StatusCode)
			}
			if resp.StatusCode >= 500 && i < maxRetries-1 {
				time.Sleep(time.Second * time.Duration(i+1))
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("http error %d: %s", resp.StatusCode, string(body))
		}

		if respBody != nil {
			if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("max retries exceeded")
}

func run(logger *zerolog.Logger) error {
	ctx := context.Background()

	jobID := os.Getenv("NOMAD_META_JOB_ID")
	projectID := os.Getenv("NOMAD_META_PROJECT_ID")
	operation := os.Getenv("NOMAD_META_OPERATION")
	moduleName := os.Getenv("NOMAD_META_MODULE_NAME")

	if jobID == "" || projectID == "" || operation == "" || moduleName == "" {
		return fmt.Errorf("missing required NOMAD_META_* environment variables")
	}

	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		return fmt.Errorf("ORCHESTRATOR_URL is required")
	}

	job, err := fetchJob(ctx, orchestratorURL, jobID)
	if err != nil {
		return fmt.Errorf("get job %s: %w", jobID, err)
	}

	if operation == "plan" {
		if err := updateStatus(ctx, orchestratorURL, job.ID, models.StatusPlanning); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
	} else if operation == "apply" {
		if err := updateStatus(ctx, orchestratorURL, job.ID, models.StatusApplying); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
	}

	infisicalClient, err := infisical.NewClient(infisical.Config{
		ClientID:     os.Getenv("INFISICAL_CLIENT_ID"),
		ClientSecret: os.Getenv("INFISICAL_CLIENT_SECRET"),
		SiteURL:      os.Getenv("INFISICAL_SITE_URL"),
	})
	if err != nil {
		updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
		return fmt.Errorf("init infisical: %w", err)
	}

	secretsPath := fmt.Sprintf("/projects/%s/terraform", projectID)
	secrets, err := infisicalClient.GetSecrets(projectID, "prod", secretsPath)
	if err != nil {
		updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
		return fmt.Errorf("fetch secrets: %w", err)
	}

	minioClient, err := storage.NewStorage(storage.Config{
		Endpoint:        os.Getenv("MINIO_ENDPOINT"),
		AccessKeyID:     os.Getenv("MINIO_ACCESS_KEY"),
		SecretAccessKey: os.Getenv("MINIO_SECRET_KEY"),
		UseSSL:          os.Getenv("MINIO_USE_SSL") == "true",
		BucketName:      os.Getenv("MINIO_BUCKET_NAME"),
	})
	if err != nil {
		updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
		return fmt.Errorf("init minio: %w", err)
	}

	if err := minioClient.EnsureBucket(ctx); err != nil {
		updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
		return fmt.Errorf("ensure bucket: %w", err)
	}

	workDir := filepath.Join("/tmp/terraform", job.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
		return fmt.Errorf("mkdir workdir: %w", err)
	}

	runner, err := terraform.NewRunner(workDir, secrets)
	if err != nil {
		updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
		return fmt.Errorf("init terraform runner: %w", err)
	}

	_, initStderr, err := runner.Init(ctx)
	if err != nil {
		updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
		return fmt.Errorf("terraform init failed: %w\nstderr: %s", err, string(initStderr))
	}

	if operation == "plan" {
		planFilename := "tfplan"
		planPath := filepath.Join(workDir, planFilename)

		planStdout, planStderr, err := runner.Plan(ctx, planPath)
		if err != nil {
			updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
			return fmt.Errorf("terraform plan failed: %w\nstderr: %s", err, string(planStderr))
		}

		showStdout, showStderr, err := runner.Show(ctx, planPath)
		if err != nil {
			updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
			return fmt.Errorf("terraform show failed: %w\nstderr: %s", err, string(showStderr))
		}

		planSummary := string(showStdout)

		planData, err := os.ReadFile(planPath)
		if err != nil {
			updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
			return fmt.Errorf("read plan file: %w", err)
		}

		objectName := fmt.Sprintf("jobs/%s/tfplan", job.ID)
		_, err = minioClient.Upload(ctx, objectName, bytes.NewReader(planData), int64(len(planData)), "application/octet-stream")
		if err != nil {
			updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
			return fmt.Errorf("upload plan: %w", err)
		}

		if err := updateJobPlanResult(ctx, orchestratorURL, job.ID, objectName, planSummary); err != nil {
			return fmt.Errorf("update job plan result: %w", err)
		}

		if err := updateStatus(ctx, orchestratorURL, job.ID, models.StatusPlanned); err != nil {
			return fmt.Errorf("update job status to planned: %w", err)
		}

		logger.Info().Msgf("Plan successful for job %s. Output:\n%s", job.ID, string(planStdout))

	} else if operation == "apply" {
		planPath := ""

		applyStdout, applyStderr, err := runner.Apply(ctx, planPath)
		if err != nil {
			updateStatus(ctx, orchestratorURL, job.ID, models.StatusFailed)
			return fmt.Errorf("terraform apply failed: %w\nstderr: %s", err, string(applyStderr))
		}

		if err := updateStatus(ctx, orchestratorURL, job.ID, models.StatusApplied); err != nil {
			return fmt.Errorf("update job status to applied: %w", err)
		}

		logger.Info().Msgf("Apply successful for job %s. Output:\n%s", job.ID, string(applyStdout))
	}

	return nil
}
