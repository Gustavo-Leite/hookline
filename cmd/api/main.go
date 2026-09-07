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

	"github.com/Gustavo-Leite/hookline/api"
	"github.com/Gustavo-Leite/hookline/internal/config"
	"github.com/Gustavo-Leite/hookline/internal/httpapi"
	"github.com/Gustavo-Leite/hookline/internal/postgres"
	"github.com/Gustavo-Leite/hookline/internal/ratelimit"
	"github.com/Gustavo-Leite/hookline/internal/redis"
)

func main() {
	slog.SetDefault(slog.New(httpapi.NewLogHandler(slog.NewJSONHandler(os.Stdout, nil))))

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
	throttle := httpapi.RateLimit(ratelimit.New(rdb, cfg.RateLimitPerMinute, cfg.RateLimitBurst))

	protected := func(next http.Handler) http.Handler {
		return authenticate(throttle(next))
	}

	mux := http.NewServeMux()

	route := func(pattern string, handler http.Handler) {
		mux.Handle(pattern, httpapi.Instrument(pattern, handler))
	}
	mux.Handle("GET /metrics", httpapi.Metrics())
	route("GET /openapi.yaml", httpapi.OpenAPISpec(api.Spec))
	route("GET /docs", http.RedirectHandler("/docs/", http.StatusMovedPermanently))
	route("GET /docs/", httpapi.Docs())
	route("GET /healthz", httpapi.Health())
	route("GET /readyz", httpapi.Ready(pool, rdb))
	route("GET /v1/me", protected(httpapi.Me()))
	endpoints := httpapi.NewEndpoints(postgres.NewEndpointStore(pool))
	route("POST /v1/endpoints", protected(http.HandlerFunc(endpoints.Create)))
	route("GET /v1/endpoints", protected(http.HandlerFunc(endpoints.List)))
	route("GET /v1/endpoints/{id}", protected(http.HandlerFunc(endpoints.Get)))
	route("PATCH /v1/endpoints/{id}", protected(http.HandlerFunc(endpoints.Update)))
	route("DELETE /v1/endpoints/{id}", protected(http.HandlerFunc(endpoints.Delete)))
	events := httpapi.NewEvents(postgres.NewEventStore(pool))
	route("POST /v1/events", protected(http.HandlerFunc(events.Create)))
	deliveries := httpapi.NewDeliveries(postgres.NewDeliveryStore(pool))
	route("GET /v1/deliveries", protected(http.HandlerFunc(deliveries.List)))
	route("GET /v1/deliveries/{id}", protected(http.HandlerFunc(deliveries.Get)))
	route("POST /v1/deliveries/{id}/replay", protected(http.HandlerFunc(deliveries.Replay)))

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           httpapi.RequestID(httpapi.LogRequests(mux)),
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
