package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/models"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) FetchJobsByStatus(ctx context.Context, status models.JobStatus) ([]*models.Job, error) {
	query := `
		SELECT id, project_id, status, operation, module_name, variables, plan_output_path, plan_summary, nomad_eval_id, created_at, updated_at
		FROM jobs
		WHERE status = $1
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("query jobs by status: %w", err)
	}
	defer rows.Close()

	var jobs []*models.Job
	for rows.Next() {
		job := &models.Job{}
		err := rows.Scan(
			&job.ID,
			&job.ProjectID,
			&job.Status,
			&job.Operation,
			&job.ModuleName,
			&job.Variables,
			&job.PlanOutputPath,
			&job.PlanSummary,
			&job.NomadEvalID,
			&job.CreatedAt,
			&job.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}

	return jobs, nil
}

func (s *Store) FetchJobsByStatuses(ctx context.Context, statuses []models.JobStatus) ([]*models.Job, error) {
	query := `
		SELECT id, project_id, status, operation, module_name, variables, plan_output_path, plan_summary, nomad_eval_id, created_at, updated_at
		FROM jobs
		WHERE status = ANY($1)
		ORDER BY created_at ASC
	`

	stringStatuses := make([]string, len(statuses))
	for i, status := range statuses {
		stringStatuses[i] = string(status)
	}

	rows, err := s.pool.Query(ctx, query, stringStatuses)
	if err != nil {
		return nil, fmt.Errorf("query jobs by statuses: %w", err)
	}
	defer rows.Close()

	var jobs []*models.Job
	for rows.Next() {
		job := &models.Job{}
		err := rows.Scan(
			&job.ID,
			&job.ProjectID,
			&job.Status,
			&job.Operation,
			&job.ModuleName,
			&job.Variables,
			&job.PlanOutputPath,
			&job.PlanSummary,
			&job.NomadEvalID,
			&job.CreatedAt,
			&job.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}

	return jobs, nil
}

func (s *Store) UpdateJobStatus(ctx context.Context, id string, status models.JobStatus) error {
	query := `
		UPDATE jobs
		SET status = $1, updated_at = NOW()
		WHERE id = $2
	`
	_, err := s.pool.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("update job status: %w", err)
	}
	return nil
}

func (s *Store) UpdateJobPlanResult(ctx context.Context, id string, planOutputPath string, planSummary string) error {
	query := `
		UPDATE jobs
		SET plan_output_path = $1, plan_summary = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err := s.pool.Exec(ctx, query, planOutputPath, planSummary, id)
	if err != nil {
		return fmt.Errorf("update job plan result: %w", err)
	}
	return nil
}

func (s *Store) UpdateJobNomadEvalID(ctx context.Context, id string, evalID string) error {
	query := `
		UPDATE jobs
		SET nomad_eval_id = $1, updated_at = NOW()
		WHERE id = $2
	`
	_, err := s.pool.Exec(ctx, query, evalID, id)
	if err != nil {
		return fmt.Errorf("update job nomad eval id: %w", err)
	}
	return nil
}

func (s *Store) GetJob(ctx context.Context, id string) (*models.Job, error) {
	query := `
		SELECT id, project_id, status, operation, module_name, variables, plan_output_path, plan_summary, nomad_eval_id, created_at, updated_at
		FROM jobs
		WHERE id = $1
	`
	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&job.ID,
		&job.ProjectID,
		&job.Status,
		&job.Operation,
		&job.ModuleName,
		&job.Variables,
		&job.PlanOutputPath,
		&job.PlanSummary,
		&job.NomadEvalID,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (s *Store) CreateJob(ctx context.Context, projectID string, operation string, moduleName string, variables map[string]interface{}) (*models.Job, error) {
	query := `
		INSERT INTO jobs (project_id, status, operation, module_name, variables)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, project_id, status, operation, module_name, variables, plan_output_path, plan_summary, nomad_eval_id, created_at, updated_at
	`
	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, projectID, models.StatusPendingPlan, operation, moduleName, variables).Scan(
		&job.ID,
		&job.ProjectID,
		&job.Status,
		&job.Operation,
		&job.ModuleName,
		&job.Variables,
		&job.PlanOutputPath,
		&job.PlanSummary,
		&job.NomadEvalID,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func (s *Store) VerifyUserAndOrg(ctx context.Context, userID, orgID string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM member m
			WHERE m.user_id = $1 AND m.organization_id = $2
		)
	`
	var exists bool
	err := s.pool.QueryRow(ctx, query, userID, orgID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("verify user and org: %w", err)
	}
	return exists, nil
}
