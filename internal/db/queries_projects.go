package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openenvx/cloud/internal/models"
)

func (s *Store) IsProjectActive(ctx context.Context, projectID string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM jobs
			WHERE project_id = $1
			AND status NOT IN ($2, $3, $4, $5)
		)
	`

	var active bool
	err := s.pool.QueryRow(ctx, query,
		projectID,
		models.StatusApplied,
		models.StatusDestroyed,
		models.StatusFailed,
		models.StatusCancelled,
	).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("check project active: %w", err)
	}

	return active, nil
}

func (s *Store) UnlockProjectState(ctx context.Context, projectID string, lockID string) error {
	// In this implementation, the project is "locked" if there is an active job.
	// To "unlock" it via the Terraform HTTP backend, we could either cancel the job
	// or just verify that the lockID matches the active job ID.
	// For now, let's verify the lockID matches the active job's ID and then we could
	// potentially mark the job as cancelled or just return success if it's already finished.
	// However, Terraform usually calls UNLOCK when it's done or when a user forces it.

	job, err := s.GetActiveJobForProject(ctx, projectID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil // Already unlocked
		}
		return fmt.Errorf("get active job for unlock: %w", err)
	}

	if job.ID != lockID {
		return fmt.Errorf("lock ID mismatch: expected %s, got %s", job.ID, lockID)
	}

	// Mark the job as cancelled to unlock the project
	err = s.UpdateJobStatus(ctx, job.ID, models.StatusCancelled)
	if err != nil {
		return fmt.Errorf("cancel job to unlock: %w", err)
	}

	return nil
}
