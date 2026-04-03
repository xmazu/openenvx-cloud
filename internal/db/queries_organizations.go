package db

import (
	"context"
	"fmt"
)

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
