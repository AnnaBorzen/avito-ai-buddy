// Command access is the standalone access-service mock. It owns its tickets and
// only exposes contracts/access-service.openapi.yaml, so the backend can test
// the real HTTP boundary instead of an in-process stub.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/accessservice"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/infrastructure/logger"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
)

const (
	defaultAddr = ":8091"
	envAddr     = "ACCESS_SERVICE_ADDR"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	addSource := false
	if raw := strings.TrimSpace(os.Getenv("LOG_ADD_SOURCE")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("LOG_ADD_SOURCE: %w", err)
		}
		addSource = parsed
	}

	log, err := logger.New(logger.Options{
		Level:     envOrDefault("LOG_LEVEL", "info"),
		Format:    envOrDefault("LOG_FORMAT", "text"),
		AddSource: addSource,
	})
	if err != nil {
		return err
	}
	logger.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer st.Close()

	return accessservice.New(envOrDefault(envAddr, defaultAddr), st, log).Run(ctx)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
