package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/openenvx/cloud/internal/auth"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/models"
)

type Server struct {
	store *db.Store
}

func NewServer(store *db.Store) *Server {
	return &Server{store: store}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /jobs", s.handleCreateJob)
	apiMux.HandleFunc("GET /jobs/", s.handleGetJob)
	apiMux.HandleFunc("POST /jobs/", s.handleApproveJob)

	mux.Handle("/internal/api/v1/", http.StripPrefix("/internal/api/v1", auth.Middleware(s.store)(apiMux)))

	return mux
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
		log.Printf("Error creating job: %v", err)
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
		log.Printf("Error getting job: %v", err)
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
		log.Printf("Error getting job: %v", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if job.Status != models.StatusPlanned {
		http.Error(w, "Job is not in PLANNED status", http.StatusBadRequest)
		return
	}

	err = s.store.UpdateJobStatus(r.Context(), id, models.StatusApproved)
	if err != nil {
		log.Printf("Error updating job status: %v", err)
		http.Error(w, "Failed to approve job", http.StatusInternalServerError)
		return
	}

	job.Status = models.StatusApproved
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}
