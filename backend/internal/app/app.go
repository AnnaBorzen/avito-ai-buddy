// Package app собирает зависимости сервиса и управляет его жизненным циклом.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/config"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/access"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/ml"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/service"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
	apihttp "github.com/AnnaBorzen/avito-ai-buddy/backend/internal/transport/http"
)

const (
	readHeaderTimeout     = 5 * time.Second
	readinessCheckTimeout = 2 * time.Second
	shutdownTimeout       = 10 * time.Second
)

// dependency описывает внешний сервис, доступность которого проверяет /readyz.
// Пустой addr означает, что проверка не настроена (например, локальный запуск без Docker).
type dependency struct {
	name string
	addr string
}

// App — HTTP-приложение с внедрёнными конфигом и логгером.
type App struct {
	cfg     config.Config
	log     *slog.Logger
	srv     *http.Server
	mux     *http.ServeMux
	seed    domain.Seed
	seedErr error
	store   *store.Store
}

// New собирает роутер и HTTP-сервер. Логгер приходит извне и используется
// всеми компонентами приложения: middleware, хендлерами и записью ответов.
func New(cfg config.Config, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", livenessHandler(log))
	mux.Handle("GET /readyz", readinessHandler(log, dependencies(cfg)))

	seed, seedErr := domain.NewSeed()
	if seedErr != nil {
		log.Error("invalid reference data", "error", seedErr)
	}

	return &App{
		cfg:     cfg,
		log:     log,
		mux:     mux,
		seed:    seed,
		seedErr: seedErr,
		srv: &http.Server{
			Addr:              cfg.HTTP.Addr(),
			Handler:           requestLogger(log, recoverPanic(log, mux)),
			ReadHeaderTimeout: readHeaderTimeout,
			// Без ErrorLog паники и ошибки net/http уходят в стандартный log мимо slog.
			ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError),
		},
	}
}

// Run запускает сервер и завершает его по отмене контекста.
// Слушающий сокет открывается до лога «server started», поэтому лог не врёт,
// если порт уже занят.
func (a *App) Run(ctx context.Context) error {
	if err := a.init(ctx); err != nil {
		return err
	}
	if a.store != nil {
		defer a.store.Close()
	}

	listener, err := net.Listen("tcp", a.srv.Addr)
	if err != nil {
		return fmt.Errorf("http listen: %w", err)
	}

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- a.srv.Serve(listener)
	}()

	a.log.Info("http server started", "addr", listener.Addr().String(), "app_env", a.cfg.App.Env)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		a.log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := a.srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	a.log.Info("http server stopped")
	return nil
}

func dependencies(cfg config.Config) []dependency {
	return []dependency{
		{name: "postgres", addr: cfg.Database.TCPAddr()},
		{name: "redis", addr: cfg.Redis.Addr},
		{name: "ml_service", addr: upstreamAddr(cfg.MLService.URL)},
		{name: "access_service", addr: upstreamAddr(cfg.AccessService.URL)},
	}
}

// init opens PostgreSQL and mounts the API. The service has no in-memory
// fallback: without DATABASE_URL only health endpoints are exposed.
func (a *App) init(ctx context.Context) error {
	if a.cfg.Database.URL == "" {
		a.log.Warn("DATABASE_URL is not set: API routes are disabled")
		return nil
	}
	if a.seedErr != nil {
		return fmt.Errorf("reference data: %w", a.seedErr)
	}

	st, err := store.Open(ctx, a.cfg.Database.URL)
	if err != nil {
		return err
	}
	a.store = st

	mlClient := ml.New(a.cfg.MLService.URL, a.cfg.MLService.Timeout)
	accessClient := access.New(a.cfg.AccessService.URL, a.cfg.AccessService.Timeout)
	svc := service.New(st, a.seed, mlClient, accessClient, a.log)
	apihttp.NewRouter(svc, a.seed, a.log).Register(a.mux)

	a.log.Info("api routes registered", "prefix", "/api/v1")
	return nil
}

func upstreamAddr(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host
}

func livenessHandler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(log, w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func readinessHandler(log *slog.Logger, deps []dependency) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dependencies := make(map[string]string, len(deps))
		ready := true

		for _, dep := range deps {
			if dep.addr == "" {
				continue
			}

			if err := dial(r.Context(), dep.addr); err != nil {
				ready = false
				dependencies[dep.name] = "unavailable"
				continue
			}

			dependencies[dep.name] = "ok"
		}

		status, state := http.StatusOK, "ok"
		if !ready {
			status, state = http.StatusServiceUnavailable, "degraded"
		}

		writeJSON(log, w, status, map[string]any{"status": state, "dependencies": dependencies})
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

// statusRecorder запоминает код ответа и объём тела, чтобы request-лог содержал
// реальный статус. Flush пробрасывается — понадобится для стриминга ответов LLM.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}

	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}

	written, err := r.ResponseWriter.Write(p)
	r.bytes += written

	return written, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		if r.status == 0 {
			r.status = http.StatusOK
		}

		flusher.Flush()
	}
}

// Status возвращает код ответа; отсутствие явного WriteHeader означает 200.
func (r *statusRecorder) Status() int {
	if r.status == 0 {
		return http.StatusOK
	}

	return r.status
}

func requestLogger(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(recorder, r)

		status := recorder.Status()
		level := slog.LevelInfo
		switch {
		case status >= http.StatusInternalServerError:
			level = slog.LevelError
		case status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}

		log.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", recorder.bytes,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

// recoverPanic не даёт панике в хендлере уронить процесс: она логируется через slog,
// а клиент получает JSON-ошибку в формате из docs/architecture.md.
func recoverPanic(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered) // штатный способ прервать ответ: логировать нечего
			}

			log.Error("panic recovered",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", recovered,
				"stack", string(debug.Stack()),
			)

			writeJSON(log, w, http.StatusInternalServerError, map[string]any{
				"error_code": "INTERNAL_ERROR",
				"message":    "Внутренняя ошибка сервиса.",
			})
		}()

		next.ServeHTTP(w, r)
	})
}

func writeJSON(log *slog.Logger, w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Error("failed to write response", "error", err)
	}
}
