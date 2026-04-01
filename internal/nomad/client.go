package nomad

import (
	"context"
	"fmt"

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

	meta := map[string]string{
		"JOB_ID":      job.ID,
		"PROJECT_ID":  job.ProjectID,
		"OPERATION":   job.Operation,
		"MODULE_NAME": job.ModuleName,
	}

	dispatchResp, _, err := c.nomad.Jobs().Dispatch(jobID, meta, nil, "", nil)
	if err != nil {
		return "", fmt.Errorf("failed to dispatch nomad job: %w", err)
	}

	return dispatchResp.EvalID, nil
}
