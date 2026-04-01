package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/openenvx/cloud/internal/db"
	"github.com/rs/zerolog"
)

type contextKey string

const (
	UserIDKey contextKey = "user_id"
	OrgIDKey  contextKey = "org_id"
)

func Middleware(store *db.Store, logger *zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, orgID, ok := r.BasicAuth()
			if !ok {
				http.Error(w, "Unauthorized: missing basic auth", http.StatusUnauthorized)
				return
			}

			userID = strings.TrimSpace(userID)
			orgID = strings.TrimSpace(orgID)

			exists, err := store.VerifyUserAndOrg(r.Context(), userID, orgID)
			if err != nil {
				logger.Error().Err(err).Msg("Error verifying user and org")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if !exists {
				http.Error(w, "Unauthorized: invalid user or organization", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, OrgIDKey, orgID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
