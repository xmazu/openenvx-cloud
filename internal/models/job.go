package models

import (
	"time"
)

type JobStatus string

const (
	StatusPendingPlan JobStatus = "PENDING_PLAN"
	StatusPlanning    JobStatus = "PLANNING"
	StatusPlanned     JobStatus = "PLANNED"
	StatusApproved    JobStatus = "APPROVED"
	StatusQueued      JobStatus = "QUEUED"
	StatusApplying    JobStatus = "APPLYING"
	StatusApplied     JobStatus = "APPLIED"
	StatusDestroying  JobStatus = "DESTROYING"
	StatusDestroyed   JobStatus = "DESTROYED"
	StatusFailed      JobStatus = "FAILED"
	StatusCancelled   JobStatus = "CANCELLED"
)

type Job struct {
	ID             string                 `json:"id" db:"id"`
	ProjectID      string                 `json:"project_id" db:"project_id"`
	OrganizationID string                 `json:"organization_id" db:"organization_id"`
	Status         JobStatus              `json:"status" db:"status"`
	Operation      string                 `json:"operation" db:"operation"`
	ModuleName     string                 `json:"module_name" db:"module_name"`
	Variables      map[string]interface{} `json:"variables" db:"variables"`
	PlanOutputPath *string                `json:"plan_output_path" db:"plan_output_path"`
	PlanSummary    *string                `json:"plan_summary" db:"plan_summary"`
	PrePlan        []string               `json:"pre_plan" db:"pre_plan"`
	PostPlan       []string               `json:"post_plan" db:"post_plan"`
	PreApply       []string               `json:"pre_apply" db:"pre_apply"`
	PostApply      []string               `json:"post_apply" db:"post_apply"`
	PreDestroy     []string               `json:"pre_destroy" db:"pre_destroy"`
	CreatedAt      time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at" db:"updated_at"`
}
