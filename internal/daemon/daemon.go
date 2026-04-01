package daemon

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/nomad"
)

type Daemon struct {
	store        *db.Store
	nomadClient  *nomad.Client
	pollInterval time.Duration
}

func NewDaemon(store *db.Store, nomadClient *nomad.Client, pollInterval time.Duration) *Daemon {
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	return &Daemon{
		store:        store,
		nomadClient:  nomadClient,
		pollInterval: pollInterval,
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	log.Printf("Starting orchestrator daemon, polling every %v", d.pollInterval)
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := d.processJobs(ctx); err != nil {
				log.Printf("Error processing jobs: %v", err)
			}
		}
	}
}

func (d *Daemon) processJobs(ctx context.Context) error {
	jobs, err := d.store.FetchJobsByStatus(ctx, models.StatusPendingPlan)
	if err != nil {
		return fmt.Errorf("fetch pending jobs: %w", err)
	}

	for _, job := range jobs {
		log.Printf("Processing job: %s", job.ID)

		evalID, err := d.nomadClient.DispatchJob(ctx, job)
		if err != nil {
			log.Printf("Failed to dispatch job %s: %v", job.ID, err)
			continue
		}

		if err := d.store.UpdateJobStatus(ctx, job.ID, models.StatusPlanning); err != nil {
			log.Printf("Failed to update job status %s: %v", job.ID, err)
			continue
		}

		if err := d.store.UpdateJobNomadEvalID(ctx, job.ID, evalID); err != nil {
			log.Printf("Failed to update nomad eval id for job %s: %v", job.ID, err)
			continue
		}

		log.Printf("Successfully dispatched job %s, EvalID: %s", job.ID, evalID)
	}

	return nil
}
