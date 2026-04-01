package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openenvx/cloud/internal/api"
	"github.com/openenvx/cloud/internal/daemon"
	"github.com/openenvx/cloud/internal/db"
	"github.com/openenvx/cloud/internal/nomad"
)

func main() {
	log.Println("Orchestrator starting...")

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
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()

	store := db.NewStore(pool)

	nomadClient, err := nomad.NewClient()
	if err != nil {
		log.Fatalf("Unable to create nomad client: %v", err)
	}

	apiServer := api.NewServer(store)
	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: apiServer.Routes(),
	}

	d := daemon.NewDaemon(store, nomadClient, 5*time.Second)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()

		// Shutdown HTTP server gracefully
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	go func() {
		log.Println("Starting HTTP API server on :8080...")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	if err := d.Start(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Daemon error: %v", err)
	}

	log.Println("Orchestrator stopped")
}
