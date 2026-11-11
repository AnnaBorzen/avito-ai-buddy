package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/app"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/config"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/infrastructure/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log, err := logger.New(logger.Options{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
	})
	if err != nil {
		return err
	}
	logger.SetDefault(log)

	if unknown := config.UnknownConfigEnvVars(); len(unknown) > 0 {
		log.Warn("unknown environment variables ignored", "vars", unknown)
	}

	log.Info("configuration loaded", "config", *cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return app.New(*cfg, log).Run(ctx)
}
