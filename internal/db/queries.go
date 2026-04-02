package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
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
		SELECT j.id, j.project_id, p.organization_id, j.status, j.operation, j.module_name, j.variables, j.plan_output_path, j.plan_summary, j.pre_plan, j.post_plan, j.pre_apply, j.post_apply, j.pre_destroy, j.created_at, j.updated_at
		FROM jobs j
		JOIN projects p ON j.project_id = p.id
		WHERE j.status = $1
		ORDER BY j.created_at ASC
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
			&job.OrganizationID,
			&job.Status,
			&job.Operation,
			&job.ModuleName,
			&job.Variables,
			&job.PlanOutputPath,
			&job.PlanSummary,
			&job.PrePlan,
			&job.PostPlan,
			&job.PreApply,
			&job.PostApply,
			&job.PreDestroy,
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
		SELECT j.id, j.project_id, p.organization_id, j.status, j.operation, j.module_name, j.variables, j.plan_output_path, j.plan_summary, j.pre_plan, j.post_plan, j.pre_apply, j.post_apply, j.pre_destroy, j.created_at, j.updated_at
		FROM jobs j
		JOIN projects p ON j.project_id = p.id
		WHERE j.status = ANY($1)
		ORDER BY j.created_at ASC
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
			&job.OrganizationID,
			&job.Status,
			&job.Operation,
			&job.ModuleName,
			&job.Variables,
			&job.PlanOutputPath,
			&job.PlanSummary,
			&job.PrePlan,
			&job.PostPlan,
			&job.PreApply,
			&job.PostApply,
			&job.PreDestroy,
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

func (s *Store) UpdateJobSummary(ctx context.Context, id string, summary string) error {
	query := `
		UPDATE jobs
		SET plan_summary = $1, updated_at = NOW()
		WHERE id = $2
	`
	_, err := s.pool.Exec(ctx, query, summary, id)
	if err != nil {
		return fmt.Errorf("update job summary: %w", err)
	}
	return nil
}

func (s *Store) GetJob(ctx context.Context, id string) (*models.Job, error) {
	query := `
		SELECT j.id, j.project_id, p.organization_id, j.status, j.operation, j.module_name, j.variables, j.plan_output_path, j.plan_summary, j.pre_plan, j.post_plan, j.pre_apply, j.post_apply, j.pre_destroy, j.created_at, j.updated_at
		FROM jobs j
		JOIN projects p ON j.project_id = p.id
		WHERE j.id = $1
	`
	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&job.ID,
		&job.ProjectID,
		&job.OrganizationID,
		&job.Status,
		&job.Operation,
		&job.ModuleName,
		&job.Variables,
		&job.PlanOutputPath,
		&job.PlanSummary,
		&job.PrePlan,
		&job.PostPlan,
		&job.PreApply,
		&job.PostApply,
		&job.PreDestroy,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (s *Store) CreateJob(ctx context.Context, projectID string, operation string, moduleName string, variables map[string]interface{}, prePlan, postPlan, preApply, postApply, preDestroy []string) (*models.Job, error) {
	query := `
		WITH new_job AS (
			INSERT INTO jobs (project_id, status, operation, module_name, variables, pre_plan, post_plan, pre_apply, post_apply, pre_destroy)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
			WHERE NOT EXISTS (
				SELECT 1 FROM jobs 
				WHERE project_id = $1 
				AND status IN ('PENDING_PLAN', 'PLANNING', 'PLANNED', 'APPROVED', 'APPLYING')
			)
			RETURNING id, project_id, status, operation, module_name, variables, plan_output_path, plan_summary, pre_plan, post_plan, pre_apply, post_apply, pre_destroy, created_at, updated_at
		)
		SELECT nj.id, nj.project_id, p.organization_id, nj.status, nj.operation, nj.module_name, nj.variables, nj.plan_output_path, nj.plan_summary, nj.pre_plan, nj.post_plan, nj.pre_apply, nj.post_apply, nj.pre_destroy, nj.created_at, nj.updated_at
		FROM new_job nj
		JOIN projects p ON nj.project_id = p.id
	`
	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, projectID, models.StatusPendingPlan, operation, moduleName, variables, prePlan, postPlan, preApply, postApply, preDestroy).Scan(
		&job.ID,
		&job.ProjectID,
		&job.OrganizationID,
		&job.Status,
		&job.Operation,
		&job.ModuleName,
		&job.Variables,
		&job.PlanOutputPath,
		&job.PlanSummary,
		&job.PrePlan,
		&job.PostPlan,
		&job.PreApply,
		&job.PostApply,
		&job.PreDestroy,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func (s *Store) GetActiveJobForProject(ctx context.Context, projectID string) (*models.Job, error) {
	query := `
		SELECT j.id, j.project_id, p.organization_id, j.status, j.operation, j.module_name, j.variables, j.plan_output_path, j.plan_summary, j.pre_plan, j.post_plan, j.pre_apply, j.post_apply, j.pre_destroy, j.created_at, j.updated_at
		FROM jobs j
		JOIN projects p ON j.project_id = p.id
		WHERE j.project_id = $1
		AND j.status IN ($2, $3, $4, $5, $6)
		LIMIT 1
	`
	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query,
		projectID,
		models.StatusPendingPlan,
		models.StatusPlanning,
		models.StatusPlanned,
		models.StatusApproved,
		models.StatusApplying,
	).Scan(
		&job.ID,
		&job.ProjectID,
		&job.OrganizationID,
		&job.Status,
		&job.Operation,
		&job.ModuleName,
		&job.Variables,
		&job.PlanOutputPath,
		&job.PlanSummary,
		&job.PrePlan,
		&job.PostPlan,
		&job.PreApply,
		&job.PostApply,
		&job.PreDestroy,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("get active job: %w", err)
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
