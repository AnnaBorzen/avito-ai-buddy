package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultPort           = "8080"
	readinessCheckTimeout = 2 * time.Second
	shutdownTimeout       = 10 * time.Second
)

// dependency описывает внешний сервис, доступность которого проверяет /readyz.
// Пустой addr означает, что проверка не настроена (например, локальный запуск без Docker).
type dependency struct {
	name string
	addr string
}

type checkResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	deps := []dependency{
		{name: "postgres", addr: postgresAddr(os.Getenv("DATABASE_URL"))},
		{name: "redis", addr: os.Getenv("REDIS_ADDR")},
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", livenessHandler())
	mux.Handle("GET /readyz", readinessHandler(deps))

	server := &http.Server{
		Addr:              ":" + envOrDefault("PORT", defaultPort),
		Handler:           requestLogger(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("http server started", "addr", server.Addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "error", err)
			os.Exit(1)
		}
		return
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("http server stopped")
}

func livenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func readinessHandler(deps []dependency) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks := make(map[string]checkResult, len(deps))
		ready := true

		for _, dep := range deps {
			if dep.addr == "" {
				checks[dep.name] = checkResult{Status: "skipped"}
				continue
			}

			if err := dial(r.Context(), dep.addr); err != nil {
				ready = false
				checks[dep.name] = checkResult{Status: "error", Error: err.Error()}
				continue
			}

			checks[dep.name] = checkResult{Status: "ok"}
		}

		status, state := http.StatusOK, "ok"
		if !ready {
			status, state = http.StatusServiceUnavailable, "degraded"
		}

		writeJSON(w, status, map[string]any{"status": state, "checks": checks})
	})
}

func dial(parent context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(parent, readinessCheckTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	return conn.Close()
}

// postgresAddr извлекает host:port из DATABASE_URL для TCP-проверки готовности.
func postgresAddr(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		slog.Warn("DATABASE_URL is not a valid connection URL, postgres readiness check disabled", "error", err)
		return ""
	}

	if parsed.Port() == "" {
		return net.JoinHostPort(parsed.Hostname(), "5432")
	}

	return parsed.Host
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
