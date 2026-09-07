package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/Gustavo-Leite/hookline/internal/config"
	"github.com/Gustavo-Leite/hookline/internal/httpapi"
	"github.com/Gustavo-Leite/hookline/internal/postgres"
	"github.com/Gustavo-Leite/hookline/internal/redis"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	pool, err := postgres.NewPool(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("postgres connected")

	rdb, err := redis.NewClient(startupCtx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()
	slog.Info("redis connected")

	authenticate := httpapi.Authenticate(postgres.NewAPIKeyStore(pool))

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", httpapi.Health())
	mux.Handle("GET /readyz", httpapi.Ready(pool, rdb))
	mux.Handle("GET /v1/me", authenticate(httpapi.Me()))
	endpoints := httpapi.NewEndpoints(postgres.NewEndpointStore(pool))
	mux.Handle("POST /v1/endpoints", authenticate(http.HandlerFunc(endpoints.Create)))
	mux.Handle("GET /v1/endpoints", authenticate(http.HandlerFunc(endpoints.List)))
	mux.Handle("GET /v1/endpoints/{id}", authenticate(http.HandlerFunc(endpoints.Get)))
	mux.Handle("DELETE /v1/endpoints/{id}", authenticate(http.HandlerFunc(endpoints.Delete)))
	events := httpapi.NewEvents(postgres.NewEventStore(pool))
	mux.Handle("POST /v1/events", authenticate(http.HandlerFunc(events.Create)))

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}

	slog.Info("shutdown complete")
	return nil
}
