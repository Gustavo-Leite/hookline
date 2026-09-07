package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/Gustavo-Leite/hookline/internal/config"
	"github.com/Gustavo-Leite/hookline/internal/delivery"
	"github.com/Gustavo-Leite/hookline/internal/postgres"
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

	worker.NewPool(postgres.NewDeliveryStore(db), sender, worker.Options{}).Run(ctx)

	slog.Info("shutdown complete")

	return nil
}
