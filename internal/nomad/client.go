package nomad

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/nomad/api"
	"github.com/openenvx/cloud/internal/models"
)

type Client struct {
	nomad *api.Client
}

func NewClient() (*Client, error) {
	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create nomad client: %w", err)
	}
	return &Client{nomad: client}, nil
}

func (c *Client) DispatchJob(ctx context.Context, job *models.Job) (string, error) {
	jobID := "terraform-worker"

	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		// Default for local development
		orchestratorURL = "http://host.docker.internal:8080"
	}

	meta := map[string]string{
		"JOB_ID":           job.ID,
		"PROJECT_ID":       job.ProjectID,
		"OPERATION":        job.Operation,
		"MODULE_NAME":      job.ModuleName,
		"ORCHESTRATOR_URL": orchestratorURL,
	}

	dispatchResp, _, err := c.nomad.Jobs().Dispatch(jobID, meta, nil, "", nil)
	if err != nil {
		return "", fmt.Errorf("failed to dispatch nomad job: %w", err)
	}

	return dispatchResp.EvalID, nil
}

func (c *Client) GetEvaluation(ctx context.Context, evalID string) (*api.Evaluation, error) {
	eval, _, err := c.nomad.Evaluations().Info(evalID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get evaluation info: %w", err)
	}
	return eval, nil
}

func (c *Client) GetAllocationsForEval(ctx context.Context, evalID string) ([]*api.AllocationListStub, error) {
	allocs, _, err := c.nomad.Evaluations().Allocations(evalID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocations for evaluation: %w", err)
	}
	return allocs, nil
}
