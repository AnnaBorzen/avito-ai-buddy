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
	"runtime/debug"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/config"
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

type checkResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// App — HTTP-приложение с внедрёнными конфигом и логгером.
type App struct {
	cfg config.Config
	log *slog.Logger
	srv *http.Server
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

	return &App{
		cfg: cfg,
		log: log,
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
	}
}

func livenessHandler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(log, w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func readinessHandler(log *slog.Logger, deps []dependency) http.Handler {
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

		writeJSON(log, w, status, map[string]any{"status": state, "checks": checks})
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
				"error": map[string]string{
					"code":    "INTERNAL_ERROR",
					"message": "Внутренняя ошибка сервиса.",
				},
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
