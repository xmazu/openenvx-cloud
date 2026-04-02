package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/openenvx/cloud/internal/auth"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/models"
	"github.com/rs/zerolog"
)

type Server struct {
	store  *db.Store
	logger *zerolog.Logger
}

func NewServer(store *db.Store, logger *zerolog.Logger) *Server {
	return &Server{store: store, logger: logger}
}

func (s *Server) Routes() http.Handler {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	api := e.Group("/internal/api/v1")
	api.Use(auth.Middleware(s.store, s.logger))

	api.POST("/jobs", s.handleCreateJob)
	api.GET("/jobs/:id", s.handleGetJob)
	api.POST("/jobs/:id/approve", s.handleApproveJob)
	api.POST("/jobs/:id/discard", s.handleDiscardJob)

	return e
}

type updateJobStatusRequest struct {
	Status models.JobStatus `json:"status"`
}

type updateJobPlanRequest struct {
	PlanOutputPath string `json:"plan_output_path"`
	PlanSummary    string `json:"plan_summary"`
}

func (s *Server) handleGetJobInternal(w http.ResponseWriter, r *http.Request) {
	s.logger.Info().Msgf("Internal API request: %s %s", r.Method, r.URL.Path)

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 5 || parts[1] != "api" || parts[2] != "internal" || parts[3] != "jobs" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	id := parts[4]

	job, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error getting job in internal API")
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handlePutJobInternal(w http.ResponseWriter, r *http.Request) {
	s.logger.Info().Msgf("Internal API request: %s %s", r.Method, r.URL.Path)

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 6 || parts[1] != "api" || parts[2] != "internal" || parts[3] != "jobs" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	id := parts[4]

	if parts[5] == "status" {
		var req updateJobStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		job, err := s.store.GetJob(r.Context(), id)
		if err != nil {
			s.logger.Error().Err(err).Msg("Error getting job in internal API")
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}

		// Terminal states should not be updated further
		if job.Status == models.StatusFailed || job.Status == models.StatusApplied || job.Status == models.StatusDestroyed || job.Status == models.StatusCancelled {
			s.logger.Info().Msgf("Job %s is already in terminal state %s, skipping update to %s", id, job.Status, req.Status)
			w.WriteHeader(http.StatusOK)
			return
		}

		err = s.store.UpdateJobStatus(r.Context(), id, req.Status)
		if err != nil {
			s.logger.Error().Err(err).Msg("Error updating job status in internal API")
			http.Error(w, "Failed to update job status", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		return
	} else if parts[5] == "plan" {
		var req updateJobPlanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		job, err := s.store.GetJob(r.Context(), id)
		if err != nil {
			s.logger.Error().Err(err).Msg("Error getting job in internal API")
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}

		// Terminal states should not be updated further
		if job.Status == models.StatusFailed || job.Status == models.StatusApplied || job.Status == models.StatusDestroyed || job.Status == models.StatusCancelled {
			s.logger.Info().Msgf("Job %s is already in terminal state %s, skipping update to plan", id, job.Status)
			w.WriteHeader(http.StatusOK)
			return
		}

		err = s.store.UpdateJobPlanResult(r.Context(), id, req.PlanOutputPath, req.PlanSummary)
		if err != nil {
			s.logger.Error().Err(err).Msg("Error updating job plan result in internal API")
			http.Error(w, "Failed to update job plan result", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

type createJobRequest struct {
	ProjectID  string                 `json:"project_id"`
	Operation  string                 `json:"operation"`
	ModuleName string                 `json:"module_name"`
	Variables  map[string]interface{} `json:"variables"`
}

func (s *Server) handleCreateJob(c echo.Context) error {
	var req createJobRequest
	if err := c.Bind(&req); err != nil {
		return c.String(http.StatusBadRequest, "Invalid request payload")
	}

	if req.ProjectID == "" || req.Operation == "" || req.ModuleName == "" {
		return c.String(http.StatusBadRequest, "Missing required fields")
	}

	job, err := s.store.CreateJob(c.Request().Context(), req.ProjectID, req.Operation, req.ModuleName, req.Variables)
	if err != nil {
		if err == pgx.ErrNoRows {
			activeJob, getErr := s.store.GetActiveJobForProject(c.Request().Context(), req.ProjectID)
			if getErr != nil {
				s.logger.Error().Err(getErr).Msg("Error getting active job for conflict response")
				return c.String(http.StatusInternalServerError, "Failed to create job")
			}
			return c.JSON(http.StatusConflict, echo.Map{
				"error":            "Project is locked",
				"locked_by_job_id": activeJob.ID,
			})
		}
		s.logger.Error().Err(err).Msg("Error creating job")
		return c.String(http.StatusInternalServerError, "Failed to create job")
	}

	return c.JSON(http.StatusCreated, job)
}

func (s *Server) handleGetJob(c echo.Context) error {
	id := c.Param("id")

	job, err := s.store.GetJob(c.Request().Context(), id)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error getting job")
		return c.String(http.StatusNotFound, "Job not found")
	}

	return c.JSON(http.StatusOK, job)
}

func (s *Server) handleApproveJob(c echo.Context) error {
	id := c.Param("id")

	job, err := s.store.GetJob(c.Request().Context(), id)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error getting job")
		return c.String(http.StatusNotFound, "Job not found")
	}

	if job.Status != models.StatusPlanned {
		return c.String(http.StatusBadRequest, "Job is not in PLANNED status")
	}

	err = s.store.UpdateJobStatus(c.Request().Context(), id, models.StatusApproved)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error updating job status")
		return c.String(http.StatusInternalServerError, "Failed to approve job")
	}

	job.Status = models.StatusApproved
	return c.JSON(http.StatusOK, job)
}

func (s *Server) handleDiscardJob(c echo.Context) error {
	id := c.Param("id")

	job, err := s.store.GetJob(c.Request().Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.String(http.StatusNotFound, "Job not found")
		}
		s.logger.Error().Err(err).Msg("Error getting job")
		return c.String(http.StatusNotFound, "Job not found")
	}

	switch job.Status {
	case models.StatusApplying, models.StatusApplied, models.StatusFailed, models.StatusCancelled:
		return c.String(http.StatusBadRequest, fmt.Sprintf("Job cannot be discarded in %s status", job.Status))
	}

	err = s.store.UpdateJobStatus(c.Request().Context(), id, models.StatusCancelled)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error updating job status")
		return c.String(http.StatusInternalServerError, "Failed to discard job")
	}

	return c.NoContent(http.StatusOK)
}
