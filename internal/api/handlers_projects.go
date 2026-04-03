package api

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/openenvx/cloud/internal/storage"
)

func (s *Server) handleGetProjectState(c echo.Context) error {
	projectID := c.Param("id")
	objectName := storage.GetProjectStatePath(projectID)

	_, err := s.storage.Stat(c.Request().Context(), objectName)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}

	reader, err := s.storage.Download(c.Request().Context(), objectName)
	if err != nil {
		s.logger.Error().Err(err).Str("project_id", projectID).Msg("Error downloading state")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to get state"})
	}
	defer reader.Close()

	return c.Stream(http.StatusOK, "application/json", reader)
}

func (s *Server) handlePostProjectState(c echo.Context) error {
	projectID := c.Param("id")
	objectName := storage.GetProjectStatePath(projectID)

	_, err := s.storage.Upload(c.Request().Context(), objectName, c.Request().Body, -1, "application/json")
	if err != nil {
		s.logger.Error().Err(err).Str("project_id", projectID).Msg("Error uploading state")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to upload state"})
	}

	return c.NoContent(http.StatusOK)
}

func (s *Server) handleLockProjectState(c echo.Context) error {
	projectID := c.Param("id")

	activeJob, err := s.store.GetActiveJobForProject(c.Request().Context(), projectID)
	if err == nil {
		return c.JSON(http.StatusLocked, echo.Map{
			"error":            "Project is locked",
			"locked_by_job_id": activeJob.ID,
		})
	}

	if err != pgx.ErrNoRows {
		s.logger.Error().Err(err).Str("project_id", projectID).Msg("Error checking active job")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal Server Error"})
	}

	return c.NoContent(http.StatusOK)
}

func (s *Server) handleUnlockProjectState(c echo.Context) error {
	projectID := c.Param("id")

	var lockPayload struct {
		ID string `json:"ID"`
	}

	if err := c.Bind(&lockPayload); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid lock payload"})
	}

	if lockPayload.ID == "" {
		lockPayload.ID = c.QueryParam("ID")
	}

	err := s.store.UnlockProjectState(c.Request().Context(), projectID, lockPayload.ID)
	if err != nil {
		s.logger.Error().Err(err).Str("project_id", projectID).Str("lock_id", lockPayload.ID).Msg("Error unlocking project state")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.NoContent(http.StatusOK)
}
