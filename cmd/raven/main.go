package main

import (
	"context"
	"encoding/json"
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
	"raven/internal/fetcher"
	"raven/internal/handler"
	"raven/internal/jobs"
	"raven/internal/model"
	"raven/internal/poller"
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

	// Build stores.
	clock := model.RealClock{}
	jobStore := store.NewJobStore(database, clock)
	feedStore := store.NewFeedStore(database, clock)
	articleStore := store.NewArticleStore(database, clock)

	// Build poller pipeline.
	fetchClient := fetcher.NewClient(fetcher.DefaultPolicy())
	p := poller.New(fetchClient, feedStore, articleStore)

	// Build feed URL map for poll handler cache.
	feeds, err := feedStore.ListPollable(ctx)
	if err != nil {
		slog.Warn("failed to list pollable feeds for URL cache", "error", err)
	}
	feedURLs := make(map[string]string, len(feeds))
	for _, f := range feeds {
		feedURLs[f.ID] = f.URL
	}

	// Register job handlers.
	pollHandler := handler.NewPollHandler(p, jobStore, feedURLs)
	handlers := map[string]jobs.Handler{
		"poll_feed": pollHandler,
	}
	worker := jobs.NewWorker(jobStore, handlers, 1)

	// Seed initial poll jobs for all due feeds.
	for _, f := range feeds {
		payload, _ := json.Marshal(handler.PollPayload{FeedID: f.ID, FeedURL: f.URL})
		dedupeKey := "poll_feed:" + f.ID
		if _, err := jobStore.Enqueue(ctx, "poll_feed", string(payload), dedupeKey); err != nil {
			slog.Error("failed to seed poll job", "feed_id", f.ID, "feed_url", f.URL, "error", err)
		}
	}
	slog.Info("seeded poll jobs", "count", len(feeds))

	// HTTP server.
	httpHandler := app.New(app.Config{
		APIToken:    cfg.APIToken,
		FeedImports: feedStore,
	})
	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      httpHandler,
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

	// Run the worker drain loop.
	slog.Info("starting worker drain loop")
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
