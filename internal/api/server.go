package api

import (
	"context"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/openenvx/cloud/internal/auth"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/storage"
	"github.com/rs/zerolog"
)

type JobStore interface {
	VerifyUserAndOrg(ctx context.Context, userID, orgID string) (bool, error)
	IsProjectActive(ctx context.Context, projectID string) (bool, error)
	CreateJob(ctx context.Context, projectID string, status models.JobStatus, operation string, moduleName string, variables map[string]interface{}, prePlan, postPlan, preApply, postApply, preDestroy []string) (*models.Job, error)
	GetJob(ctx context.Context, id string) (*models.Job, error)
	UpdateJobStatus(ctx context.Context, id string, status models.JobStatus) error
	GetActiveJobForProject(ctx context.Context, projectID string) (*models.Job, error)
	ClaimNextJob(ctx context.Context, statuses []models.JobStatus) (*models.Job, error)
	UnlockProjectState(ctx context.Context, projectID string, lockID string) error
}

type ObjectStorage interface {
	Stat(ctx context.Context, objectName string) (storage.ObjectInfo, error)
	Download(ctx context.Context, objectName string) (io.ReadCloser, error)
	Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error)
}

type MessageBroker interface {
	Subscribe(id string) chan string
	Unsubscribe(id string, ch chan string)
}

type Server struct {
	store       JobStore
	storage     ObjectStorage
	logger      zerolog.Logger
	broker      MessageBroker
	systemToken string
}

func NewServer(store JobStore, storage ObjectStorage, logger zerolog.Logger, broker MessageBroker, systemToken string) *Server {
	return &Server{store: store, storage: storage, logger: logger, broker: broker, systemToken: systemToken}
}

func (s *Server) Routes() http.Handler {
	e := echo.New()

	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			s.logger.Info().
				Str("URI", v.URI).
				Int("status", v.Status).
				Msg("request")
			return nil
		},
	}))
	e.Use(middleware.Recover())

	api := e.Group("/internal/api/v1")
	api.Use(auth.Middleware(s.store, s.systemToken, s.logger))

	api.POST("/jobs", s.handleCreateJob)
	api.GET("/jobs/:id", s.handleGetJob)
	api.GET("/jobs/:id/logs/stream", s.handleStreamJobLogs)
	api.POST("/jobs/:id/approve", s.handleApproveJob)
	api.POST("/jobs/:id/discard", s.handleDiscardJob)

	api.GET("/projects/:id/state", s.handleGetProjectState)
	api.POST("/projects/:id/state", s.handlePostProjectState)
	api.Match([]string{"LOCK"}, "/projects/:id/state", s.handleLockProjectState)
	api.Match([]string{"UNLOCK"}, "/projects/:id/state", s.handleUnlockProjectState)

	return e
}
