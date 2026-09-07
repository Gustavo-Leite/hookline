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
	"github.com/Gustavo-Leite/hookline/internal/delivery"
	"github.com/Gustavo-Leite/hookline/internal/httpapi"
	"github.com/Gustavo-Leite/hookline/internal/postgres"
	"github.com/Gustavo-Leite/hookline/internal/secrets"
	"github.com/Gustavo-Leite/hookline/internal/worker"
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

	cipher, err := secrets.NewCipher(cfg.SecretEncryptionKey)
	if err != nil {
		return err
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	db, err := postgres.NewPool(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	slog.Info("postgres connected")

	sender := delivery.NewSender(delivery.SenderOptions{
		AllowPrivateTargets: cfg.AllowPrivateDeliveryTargets,
	})

	if cfg.AllowPrivateDeliveryTargets {
		slog.Warn("delivering to private addresses is allowed; never do this in production")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	metricsServer := &http.Server{
		Addr:              ":" + cfg.MetricsPort,
		Handler:           httpapi.Metrics(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("metrics server listening", "addr", metricsServer.Addr)

		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server stopped", "error", err)
		}
	}()

	worker.NewPool(postgres.NewDeliveryStore(db, cipher), sender, worker.Options{}).Run(ctx)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("metrics server shutdown: %w", err)
	}

	slog.Info("shutdown complete")

	return nil
}
