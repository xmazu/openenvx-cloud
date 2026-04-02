package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/openenvx/cloud/internal/db"
	"github.com/rs/zerolog"
)

type contextKey string

const (
	UserIDKey contextKey = "user_id"
	OrgIDKey  contextKey = "org_id"
)

func Middleware(store *db.Store, logger *zerolog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, orgID, ok := c.Request().BasicAuth()
			if !ok {
				return c.String(http.StatusUnauthorized, "Unauthorized: missing basic auth")
			}

			userID = strings.TrimSpace(userID)
			orgID = strings.TrimSpace(orgID)

			exists, err := store.VerifyUserAndOrg(c.Request().Context(), userID, orgID)
			if err != nil {
				logger.Error().Err(err).Msg("Error verifying user and org")
				return c.String(http.StatusInternalServerError, "Internal Server Error")
			}

			if !exists {
				return c.String(http.StatusUnauthorized, "Unauthorized: invalid user or organization")
			}

			c.Set(string(UserIDKey), userID)
			c.Set(string(OrgIDKey), orgID)

			ctx := context.WithValue(c.Request().Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, OrgIDKey, orgID)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}
