package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openenvx/cloud/internal/models"
)

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

func (s *Store) CreateJob(ctx context.Context, projectID string, status models.JobStatus, operation string, moduleName string, variables map[string]interface{}, prePlan, postPlan, preApply, postApply, preDestroy []string) (*models.Job, error) {
	query := `
		WITH new_job AS (
			INSERT INTO jobs (project_id, status, operation, module_name, variables, pre_plan, post_plan, pre_apply, post_apply, pre_destroy)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id, project_id, status, operation, module_name, variables, plan_output_path, plan_summary, pre_plan, post_plan, pre_apply, post_apply, pre_destroy, created_at, updated_at
		)
		SELECT nj.id, nj.project_id, p.organization_id, nj.status, nj.operation, nj.module_name, nj.variables, nj.plan_output_path, nj.plan_summary, nj.pre_plan, nj.post_plan, nj.pre_apply, nj.post_apply, nj.pre_destroy, nj.created_at, nj.updated_at
		FROM new_job nj
		JOIN projects p ON nj.project_id = p.id
	`
	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, projectID, status, operation, moduleName, variables, prePlan, postPlan, preApply, postApply, preDestroy).Scan(
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

func (s *Store) PromoteNextJob(ctx context.Context, projectID string) (*models.Job, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin promote next job tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	query := `
		WITH next_job AS (
			SELECT j.id
			FROM jobs j
			WHERE j.project_id = $1
			AND j.status = $2
			ORDER BY j.created_at ASC, j.id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), updated AS (
			UPDATE jobs j
			SET status = $3, updated_at = NOW()
			FROM next_job
			WHERE j.id = next_job.id
			RETURNING j.id, j.project_id, j.status, j.operation, j.module_name, j.variables, j.plan_output_path, j.plan_summary, j.pre_plan, j.post_plan, j.pre_apply, j.post_apply, j.pre_destroy, j.created_at, j.updated_at
		)
		SELECT u.id, u.project_id, p.organization_id, u.status, u.operation, u.module_name, u.variables, u.plan_output_path, u.plan_summary, u.pre_plan, u.post_plan, u.pre_apply, u.post_apply, u.pre_destroy, u.created_at, u.updated_at
		FROM updated u
		JOIN projects p ON u.project_id = p.id
	`

	job := &models.Job{}
	err = tx.QueryRow(ctx, query, projectID, models.StatusQueued, models.StatusPendingPlan).Scan(
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
		return nil, fmt.Errorf("promote next job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit promote next job tx: %w", err)
	}

	return job, nil
}

func (s *Store) FailTimedOutJobs(ctx context.Context, timeout time.Duration) (int64, error) {
	query := `
		UPDATE jobs
		SET status = $1, updated_at = NOW()
		WHERE status = $2
		AND created_at < NOW() - $3::interval
	`
	res, err := s.pool.Exec(ctx, query, models.StatusFailed, models.StatusQueued, fmt.Sprintf("%d seconds", int(timeout.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("fail timed out jobs: %w", err)
	}
	return res.RowsAffected(), nil
}

func (s *Store) ClaimNextJob(ctx context.Context, statuses []models.JobStatus) (*models.Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	query := `
		WITH next_job AS (
			SELECT j.id
			FROM jobs j
			WHERE j.status = ANY($1)
			ORDER BY j.created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), updated AS (
			UPDATE jobs j
			SET status = CASE 
				WHEN j.operation = 'plan' THEN $2::job_status
				WHEN j.operation = 'apply' THEN $3::job_status
				WHEN j.operation = 'destroy' THEN $4::job_status
				ELSE j.status
			END, 
			updated_at = NOW()
			FROM next_job
			WHERE j.id = next_job.id
			RETURNING j.id, j.project_id, j.status, j.operation, j.module_name, j.variables, j.plan_output_path, j.plan_summary, j.pre_plan, j.post_plan, j.pre_apply, j.post_apply, j.pre_destroy, j.created_at, j.updated_at
		)
		SELECT u.id, u.project_id, p.organization_id, u.status, u.operation, u.module_name, u.variables, u.plan_output_path, u.plan_summary, u.pre_plan, u.post_plan, u.pre_apply, u.post_apply, u.pre_destroy, u.created_at, u.updated_at
		FROM updated u
		JOIN projects p ON u.project_id = p.id
	`

	stringStatuses := make([]string, len(statuses))
	for i, status := range statuses {
		stringStatuses[i] = string(status)
	}

	job := &models.Job{}
	err = tx.QueryRow(ctx, query,
		stringStatuses,
		models.StatusPlanning,
		models.StatusApplying,
		models.StatusDestroying,
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
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return job, nil
}
