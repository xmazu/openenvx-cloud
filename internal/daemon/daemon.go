package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/models"
	"github.com/openenvx/cloud/internal/nomad"
	"github.com/rs/zerolog"
)

type Daemon struct {
	store        *db.Store
	nomadClient  *nomad.Client
	pollInterval time.Duration
	logger       *zerolog.Logger
}

func NewDaemon(store *db.Store, nomadClient *nomad.Client, pollInterval time.Duration, logger *zerolog.Logger) *Daemon {
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	return &Daemon{
		store:        store,
		nomadClient:  nomadClient,
		pollInterval: pollInterval,
		logger:       logger,
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	d.logger.Info().Msgf("Starting orchestrator daemon, polling every %v", d.pollInterval)
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := d.processJobs(ctx); err != nil {
				d.logger.Error().Err(err).Msg("Error processing jobs")
			}
			if err := d.monitorActiveJobs(ctx); err != nil {
				d.logger.Error().Err(err).Msg("Error monitoring active jobs")
			}
		}
	}
}

func (d *Daemon) monitorActiveJobs(ctx context.Context) error {
	activeStatuses := []models.JobStatus{
		models.StatusPlanning,
		models.StatusApplying,
		models.StatusDestroying,
	}

	jobs, err := d.store.FetchJobsByStatuses(ctx, activeStatuses)
	if err != nil {
		return fmt.Errorf("fetch active jobs: %w", err)
	}

	for _, job := range jobs {
		logger := d.logger.With().Str("id", job.ID).Str("status", string(job.Status)).Logger()

		if time.Since(job.UpdatedAt) > 30*time.Minute {
			logger.Warn().Msg("Job timed out (active for > 30 mins), marking as FAILED")
			if err := d.store.UpdateJobStatus(ctx, job.ID, models.StatusFailed); err != nil {
				logger.Error().Err(err).Msg("Failed to update job status to FAILED")
			}
			continue
		}

		if job.NomadEvalID == nil || *job.NomadEvalID == "" {
			logger.Warn().Msg("Job missing Nomad Eval ID, marking as FAILED")
			if err := d.store.UpdateJobStatus(ctx, job.ID, models.StatusFailed); err != nil {
				logger.Error().Err(err).Msg("Failed to update job status to FAILED")
			}
			continue
		}

		evalID := *job.NomadEvalID
		eval, err := d.nomadClient.GetEvaluation(ctx, evalID)
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				logger.Warn().Err(err).Str("eval_id", evalID).Msg("Nomad evaluation missing (404), marking as FAILED")
				if updateErr := d.store.UpdateJobStatus(ctx, job.ID, models.StatusFailed); updateErr != nil {
					logger.Error().Err(updateErr).Msg("Failed to update job status to FAILED")
				}
			} else {
				logger.Error().Err(err).Str("eval_id", evalID).Msg("Failed to get Nomad evaluation")
			}
			continue
		}

		if eval.Status == "failed" || eval.Status == "cancelled" {
			logger.Warn().Str("eval_status", eval.Status).Msgf("Nomad evaluation %s, marking as FAILED", eval.Status)
			if err := d.store.UpdateJobStatus(ctx, job.ID, models.StatusFailed); err != nil {
				logger.Error().Err(err).Msg("Failed to update job status to FAILED")
			}
			continue
		}

		allocs, err := d.nomadClient.GetAllocationsForEval(ctx, evalID)
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				logger.Warn().Err(err).Str("eval_id", evalID).Msg("Nomad allocations missing (404), marking as FAILED")
				if updateErr := d.store.UpdateJobStatus(ctx, job.ID, models.StatusFailed); updateErr != nil {
					logger.Error().Err(updateErr).Msg("Failed to update job status to FAILED")
				}
			} else {
				logger.Error().Err(err).Str("eval_id", evalID).Msg("Failed to get allocations for evaluation")
			}
			continue
		}

		hasFailedAlloc := false
		for _, alloc := range allocs {
			if alloc.ClientStatus == "failed" {
				hasFailedAlloc = true
				break
			}
		}

		if hasFailedAlloc {
			logger.Warn().Msg("Job has failed allocations, marking as FAILED")
			if err := d.store.UpdateJobStatus(ctx, job.ID, models.StatusFailed); err != nil {
				logger.Error().Err(err).Msg("Failed to update job status to FAILED")
			}
			continue
		}
	}

	return nil
}

func (d *Daemon) processJobs(ctx context.Context) error {
	jobs, err := d.store.FetchJobsByStatus(ctx, models.StatusPendingPlan)
	if err != nil {
		return fmt.Errorf("fetch pending jobs: %w", err)
	}

	for _, job := range jobs {
		d.logger.Info().Str("id", job.ID).Msg("Processing job")

		evalID, err := d.nomadClient.DispatchJob(ctx, job)
		if err != nil {
			d.logger.Error().Str("id", job.ID).Err(err).Msg("Failed to dispatch job")
			continue
		}

		if err := d.store.UpdateJobStatus(ctx, job.ID, models.StatusPlanning); err != nil {
			d.logger.Error().Str("id", job.ID).Err(err).Msg("Failed to update job status")
			continue
		}

		if err := d.store.UpdateJobNomadEvalID(ctx, job.ID, evalID); err != nil {
			d.logger.Error().Str("id", job.ID).Err(err).Msg("Failed to update nomad eval id for job")
			continue
		}

		d.logger.Info().Str("id", job.ID).Str("eval_id", evalID).Msg("Successfully dispatched job")
	}

	return nil
}
