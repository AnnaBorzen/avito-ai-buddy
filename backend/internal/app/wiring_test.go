package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/config"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/infrastructure/logger"
)

// configEnvNames — переменные окружения, которые читает config.Load.
// Тесты выставляют их явно, чтобы не зависеть от окружения разработчика.
var configEnvNames = []string{
	"APP_ENV",
	"PORT",
	"DATABASE_URL",
	"REDIS_ADDR",
	"LOG_LEVEL",
	"LOG_FORMAT",
	"LOG_ADD_SOURCE",
	"LLM_ENABLED",
	"LLM_BASE_URL",
	"LLM_API_KEY",
	"LLM_MODEL",
	"LLM_TIMEOUT",
	"MCP_ENABLED",
	"MCP_URL",
	"MCP_TIMEOUT",
}

// TestLoadedConfigBuildsLogger — контракт между config и logger:
// значения из конфига должны приниматься логгером без дополнительных преобразований.
func TestLoadedConfigBuildsLogger(t *testing.T) {
	tests := []struct {
		name     string
		appEnv   string
		wantJSON bool
	}{
		{name: "development uses text format", appEnv: config.EnvDev, wantJSON: false},
		{name: "production uses json format", appEnv: config.EnvProd, wantJSON: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range configEnvNames {
				t.Setenv(name, "")
			}

			t.Setenv("APP_ENV", test.appEnv)

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error: %v", err)
			}

			var buf bytes.Buffer

			log, err := logger.New(logger.Options{
				Level:     cfg.Log.Level,
				Format:    cfg.Log.Format,
				AddSource: cfg.Log.AddSource,
				Output:    &buf,
			})
			if err != nil {
				t.Fatalf("logger.New() must accept config values, got error: %v", err)
			}

			log.Info("probe")

			out := buf.String()
			if isJSON := strings.HasPrefix(out, "{"); isJSON != test.wantJSON {
				t.Fatalf("unexpected log output for %s: %q", test.appEnv, out)
			}
		})
	}
}

// TestFormatConstantsMatchLogger фиксирует совпадение строковых форматов:
// config передаёт их в logger, поэтому расхождение — ошибка контракта.
func TestFormatConstantsMatchLogger(t *testing.T) {
	if config.FormatJSON != logger.FormatJSON {
		t.Errorf("config.FormatJSON = %q, logger.FormatJSON = %q", config.FormatJSON, logger.FormatJSON)
	}

	if config.FormatText != logger.FormatText {
		t.Errorf("config.FormatText = %q, logger.FormatText = %q", config.FormatText, logger.FormatText)
	}
}

func TestNewSetsServerErrorLog(t *testing.T) {
	application := New(config.Config{App: config.AppConfig{Env: config.EnvDev}}, testLogger())

	if application.srv.ErrorLog == nil {
		t.Fatal("http.Server.ErrorLog must be set, otherwise net/http logs bypass slog")
	}
}

func newBufferedLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func TestRequestLoggerRecordsStatus(t *testing.T) {
	var buf bytes.Buffer

	handler := requestLogger(newBufferedLogger(&buf), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("nope"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))

	out := buf.String()
	for _, want := range []string{"status=429", "bytes=4", "level=WARN", "path=/readyz"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output %q does not contain %q", out, want)
		}
	}
}

func TestRequestLoggerUsesImplicitOK(t *testing.T) {
	var buf bytes.Buffer

	handler := requestLogger(newBufferedLogger(&buf), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	out := buf.String()
	for _, want := range []string{"status=200", "level=INFO"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output %q does not contain %q", out, want)
		}
	}
}

func TestRecoverPanicReturnsJSONError(t *testing.T) {
	var buf bytes.Buffer

	handler := recoverPanic(newBufferedLogger(&buf), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: got %d, want %d", recorder.Code, http.StatusInternalServerError)
	}

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not valid json: %v (%q)", err, recorder.Body.String())
	}

	if payload.Error.Code != "INTERNAL_ERROR" || payload.Error.Message == "" {
		t.Fatalf("unexpected error payload: %+v", payload)
	}

	out := buf.String()
	for _, want := range []string{"panic recovered", "panic=boom", "stack="} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output %q does not contain %q", out, want)
		}
	}
}

// TestHandlerLogsRecoveredPanicAsError проверяет порядок middleware:
// request-лог должен увидеть 500, который выставил recoverPanic.
func TestHandlerLogsRecoveredPanicAsError(t *testing.T) {
	var buf bytes.Buffer
	log := newBufferedLogger(&buf)

	handler := requestLogger(log, recoverPanic(log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))

	out := buf.String()
	for _, want := range []string{"status=500", "level=ERROR", "path=/boom"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output %q does not contain %q", out, want)
		}
	}
}

// TestRunReportsListenError проверяет, что занятый порт не маскируется логом о старте:
// Run возвращает ошибку и не сообщает об успешном запуске.
func TestRunReportsListenError(t *testing.T) {
	busyAddr := startTCPServer(t)

	cfg := config.Config{App: config.AppConfig{Env: config.EnvDev}}

	var buf bytes.Buffer
	application := New(cfg, newBufferedLogger(&buf))
	application.srv.Addr = busyAddr

	if err := application.Run(context.Background()); err == nil {
		t.Fatal("Run() must fail when the address is already in use")
	}

	if out := buf.String(); strings.Contains(out, "http server started") {
		t.Fatalf("Run() must not log successful start, got %q", out)
	}
}
