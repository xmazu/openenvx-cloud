package api

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/openenvx/cloud/internal/models"
)

type createJobRequest struct {
	ProjectID  string                 `json:"project_id"`
	Operation  string                 `json:"operation"`
	ModuleName string                 `json:"module_name"`
	Variables  map[string]interface{} `json:"variables"`
	PrePlan    []string               `json:"pre_plan"`
	PostPlan   []string               `json:"post_plan"`
	PreApply   []string               `json:"pre_apply"`
	PostApply  []string               `json:"post_apply"`
	PreDestroy []string               `json:"pre_destroy"`
}

func (s *Server) handleCreateJob(c echo.Context) error {
	var req createJobRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request payload"})
	}

	if req.ProjectID == "" || req.Operation == "" || req.ModuleName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Missing required fields"})
	}

	isActive, err := s.store.IsProjectActive(c.Request().Context(), req.ProjectID)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error checking if project is active")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to create job"})
	}

	status := models.StatusPendingPlan
	if isActive {
		status = models.StatusQueued
	}

	job, err := s.store.CreateJob(c.Request().Context(), req.ProjectID, status, req.Operation, req.ModuleName, req.Variables, req.PrePlan, req.PostPlan, req.PreApply, req.PostApply, req.PreDestroy)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error creating job")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to create job"})
	}

	return c.JSON(http.StatusCreated, job)
}

func (s *Server) handleGetJob(c echo.Context) error {
	id := c.Param("id")

	job, err := s.store.GetJob(c.Request().Context(), id)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error getting job")
		return c.JSON(http.StatusNotFound, echo.Map{"error": "Job not found"})
	}

	return c.JSON(http.StatusOK, job)
}

func (s *Server) handleStreamJobLogs(c echo.Context) error {
	id := c.Param("id")

	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	ch := s.broker.Subscribe(id)
	defer s.broker.Unsubscribe(id, ch)

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case line, ok := <-ch:
			if !ok {
				return nil
			}
			if _, err := fmt.Fprintf(c.Response(), "data: %s\n\n", line); err != nil {
				return err
			}
			c.Response().Flush()
		}
	}
}

func (s *Server) handleApproveJob(c echo.Context) error {
	id := c.Param("id")

	job, err := s.store.GetJob(c.Request().Context(), id)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error getting job")
		return c.JSON(http.StatusNotFound, echo.Map{"error": "Job not found"})
	}

	if job.Status != models.StatusPlanned {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Job is not in PLANNED status"})
	}

	err = s.store.UpdateJobStatus(c.Request().Context(), id, models.StatusApproved)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error updating job status")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to approve job"})
	}

	job.Status = models.StatusApproved
	return c.JSON(http.StatusOK, job)
}

func (s *Server) handleDiscardJob(c echo.Context) error {
	id := c.Param("id")

	job, err := s.store.GetJob(c.Request().Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Job not found"})
		}
		s.logger.Error().Err(err).Msg("Error getting job")
		return c.JSON(http.StatusNotFound, echo.Map{"error": "Job not found"})
	}

	switch job.Status {
	case models.StatusApplying, models.StatusApplied, models.StatusFailed, models.StatusCancelled:
		return c.JSON(http.StatusBadRequest, echo.Map{"error": fmt.Sprintf("Job cannot be discarded in %s status", job.Status)})
	}

	err = s.store.UpdateJobStatus(c.Request().Context(), id, models.StatusCancelled)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error updating job status")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to discard job"})
	}

	return c.NoContent(http.StatusOK)
}
