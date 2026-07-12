package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"raven/internal/app"
	"raven/internal/config"
	"raven/internal/db"
	"raven/internal/jobs"
	"raven/internal/model"
	"raven/internal/store"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	slog.Info("starting raven", "config", cfg)

	// Ensure data directory exists.
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		log.Fatalf("failed to create data directory %q: %v", cfg.DataDir, err)
	}

	// Open and migrate the database.
	dbPath := filepath.Join(cfg.DataDir, "raven.db")
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
		}
	}()

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}
	slog.Info("database migrated", "path", dbPath)

	// Build the worker.
	clock := model.RealClock{}
	jobStore := store.NewJobStore(database, clock)
	feedStore := store.NewFeedStore(database, clock)

	// Empty handler map until feed jobs are added. Unknown persisted jobs
	// will be failed visibly with a descriptive error by the worker.
	handlers := map[string]jobs.Handler{}
	worker := jobs.NewWorker(jobStore, handlers, 1)

	// HTTP server.
	handler := app.New(app.Config{
		APIToken:    cfg.APIToken,
		FeedImports: feedStore,
	})
	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.RequestTimeout,
		WriteTimeout: cfg.RequestTimeout,
		IdleTimeout:  2 * cfg.RequestTimeout,
	}

	// Start server in a goroutine.
	go func() {
		slog.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Create a context that is cancelled on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	runCtx, stop := context.WithCancel(context.Background())

	go func() {
		sig := <-quit
		slog.Info("received signal, shutting down", "signal", sig)
		stop()
	}()

	// Run a periodic worker-drain loop under the signal-cancelled context.
	// Worker.Run drains all eligible jobs and returns. We loop with a poll
	// interval so new jobs are picked up promptly. DrainLoop uses a
	// context-aware select so cancellation during the wait is prompt.
	slog.Info("starting worker drain loop")
	// Keep the production interval at 5 seconds; tests inject a shorter one.
	jobs.DrainLoop(runCtx, worker.Run, 5*time.Second)
	slog.Info("worker loop stopped")

	// Graceful HTTP shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.RequestTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	slog.Info("server stopped gracefully")
}
