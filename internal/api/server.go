package api

import (
	"encoding/json"
	"net/http"
	"strings"

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
	mux := http.NewServeMux()

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /jobs", s.handleCreateJob)
	apiMux.HandleFunc("GET /jobs/", s.handleGetJob)
	apiMux.HandleFunc("POST /jobs/", s.handleApproveJob)

	mux.Handle("/internal/api/v1/", http.StripPrefix("/internal/api/v1", auth.Middleware(s.store, s.logger)(apiMux)))

	// Internal API (for workers) - No complex auth
	mux.HandleFunc("GET /api/internal/jobs/", s.handleGetJobInternal)
	mux.HandleFunc("PUT /api/internal/jobs/", s.handlePutJobInternal)

	return mux
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

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if req.ProjectID == "" || req.Operation == "" || req.ModuleName == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	job, err := s.store.CreateJob(r.Context(), req.ProjectID, req.Operation, req.ModuleName, req.Variables)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error creating job")
		http.Error(w, "Failed to create job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 || parts[1] != "jobs" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	id := parts[2]

	job, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error getting job")
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleApproveJob(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 4 || parts[1] != "jobs" || parts[3] != "approve" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	id := parts[2]

	job, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error getting job")
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if job.Status != models.StatusPlanned {
		http.Error(w, "Job is not in PLANNED status", http.StatusBadRequest)
		return
	}

	err = s.store.UpdateJobStatus(r.Context(), id, models.StatusApproved)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error updating job status")
		http.Error(w, "Failed to approve job", http.StatusInternalServerError)
		return
	}

	job.Status = models.StatusApproved
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}
