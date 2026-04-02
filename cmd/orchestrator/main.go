package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/api"
	"github.com/openenvx/cloud/internal/daemon"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/infisical"
	"github.com/openenvx/cloud/internal/storage"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	logger.Info().Msg("Orchestrator starting...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/openenvx?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to connect to database")
	}
	defer pool.Close()

	store := db.NewStore(pool)

	infisicalClient, err := infisical.NewClient(infisical.Config{
		ClientID:     mustGetEnv("INFISICAL_CLIENT_ID", logger),
		ClientSecret: mustGetEnv("INFISICAL_CLIENT_SECRET", logger),
		SiteURL:      mustGetEnv("INFISICAL_SITE_URL", logger),
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to create infisical client")
	}

	minioUseSSL, err := strconv.ParseBool(envOrDefault("MINIO_USE_SSL", "false"))
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to parse MINIO_USE_SSL")
	}

	storageClient, err := storage.NewStorage(storage.Config{
		Endpoint:        mustGetEnv("MINIO_ENDPOINT", logger),
		AccessKeyID:     mustGetEnv("MINIO_ACCESS_KEY", logger),
		SecretAccessKey: mustGetEnv("MINIO_SECRET_KEY", logger),
		UseSSL:          minioUseSSL,
		BucketName:      mustGetEnv("MINIO_BUCKET_NAME", logger),
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to create storage client")
	}

	apiServer := api.NewServer(store, &logger)
	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: apiServer.Routes(),
	}

	d := daemon.NewDaemon(store, infisicalClient, storageClient, 5, 5*time.Second, &logger)

	go func() {
		<-sigChan
		logger.Info().Msg("Shutting down...")
		cancel()

		// Shutdown HTTP server gracefully
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error().Err(err).Msg("HTTP server shutdown error")
		}
	}()

	go func() {
		logger.Info().Msg("Starting HTTP API server on :8080...")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	if err := d.Start(ctx); err != nil && err != context.Canceled {
		logger.Fatal().Err(err).Msg("Daemon error")
	}

	logger.Info().Msg("Orchestrator stopped")
}

func mustGetEnv(key string, logger zerolog.Logger) string {
	value := os.Getenv(key)
	if value == "" {
		logger.Fatal().Str("key", key).Msg("Required environment variable is missing")
	}

	return value
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
