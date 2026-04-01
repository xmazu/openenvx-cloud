package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/infisical"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/storage"
	"github.com/openenvx/cloud/internal/terraform"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("worker failed: %v", err)
	}
}

func run() error {
	ctx := context.Background()

	jobID := os.Getenv("NOMAD_META_JOB_ID")
	projectID := os.Getenv("NOMAD_META_PROJECT_ID")
	operation := os.Getenv("NOMAD_META_OPERATION")
	moduleName := os.Getenv("NOMAD_META_MODULE_NAME")

	if jobID == "" || projectID == "" || operation == "" || moduleName == "" {
		return fmt.Errorf("missing required NOMAD_META_* environment variables")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to db: %w", err)
	}
	defer pool.Close()
	store := db.NewStore(pool)

	job, err := store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get job %s: %w", jobID, err)
	}

	if operation == "plan" {
		if err := store.UpdateJobStatus(ctx, job.ID, models.StatusPlanning); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
	} else if operation == "apply" {
		if err := store.UpdateJobStatus(ctx, job.ID, models.StatusApplying); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
	}

	infisicalClient, err := infisical.NewClient(infisical.Config{
		ClientID:     os.Getenv("INFISICAL_CLIENT_ID"),
		ClientSecret: os.Getenv("INFISICAL_CLIENT_SECRET"),
		SiteURL:      os.Getenv("INFISICAL_SITE_URL"),
	})
	if err != nil {
		store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
		return fmt.Errorf("init infisical: %w", err)
	}

	secretsPath := fmt.Sprintf("/projects/%s/terraform", projectID)
	secrets, err := infisicalClient.GetSecrets(projectID, "prod", secretsPath)
	if err != nil {
		store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
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
		store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
		return fmt.Errorf("init minio: %w", err)
	}

	if err := minioClient.EnsureBucket(ctx); err != nil {
		store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
		return fmt.Errorf("ensure bucket: %w", err)
	}

	workDir := filepath.Join("/tmp/terraform", job.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
		return fmt.Errorf("mkdir workdir: %w", err)
	}

	runner, err := terraform.NewRunner(workDir, secrets)
	if err != nil {
		store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
		return fmt.Errorf("init terraform runner: %w", err)
	}

	_, initStderr, err := runner.Init(ctx)
	if err != nil {
		store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
		return fmt.Errorf("terraform init failed: %w\nstderr: %s", err, string(initStderr))
	}

	if operation == "plan" {
		planFilename := "tfplan"
		planPath := filepath.Join(workDir, planFilename)

		planStdout, planStderr, err := runner.Plan(ctx, planPath)
		if err != nil {
			store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
			return fmt.Errorf("terraform plan failed: %w\nstderr: %s", err, string(planStderr))
		}

		showStdout, showStderr, err := runner.Show(ctx, planPath)
		if err != nil {
			store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
			return fmt.Errorf("terraform show failed: %w\nstderr: %s", err, string(showStderr))
		}

		planSummary := string(showStdout)

		planData, err := os.ReadFile(planPath)
		if err != nil {
			store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
			return fmt.Errorf("read plan file: %w", err)
		}

		objectName := fmt.Sprintf("jobs/%s/tfplan", job.ID)
		_, err = minioClient.Upload(ctx, objectName, bytes.NewReader(planData), int64(len(planData)), "application/octet-stream")
		if err != nil {
			store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
			return fmt.Errorf("upload plan: %w", err)
		}

		if err := store.UpdateJobPlanResult(ctx, job.ID, objectName, planSummary); err != nil {
			return fmt.Errorf("update job plan result: %w", err)
		}

		if err := store.UpdateJobStatus(ctx, job.ID, models.StatusPlanned); err != nil {
			return fmt.Errorf("update job status to planned: %w", err)
		}

		log.Printf("Plan successful for job %s. Output:\n%s", job.ID, string(planStdout))

	} else if operation == "apply" {
		planPath := ""

		applyStdout, applyStderr, err := runner.Apply(ctx, planPath)
		if err != nil {
			store.UpdateJobStatus(ctx, job.ID, models.StatusFailed)
			return fmt.Errorf("terraform apply failed: %w\nstderr: %s", err, string(applyStderr))
		}

		if err := store.UpdateJobStatus(ctx, job.ID, models.StatusApplied); err != nil {
			return fmt.Errorf("update job status to applied: %w", err)
		}

		log.Printf("Apply successful for job %s. Output:\n%s", job.ID, string(applyStdout))
	}

	return nil
}
