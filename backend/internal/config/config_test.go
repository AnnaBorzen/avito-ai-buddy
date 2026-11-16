package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

var allEnvKeys = []string{
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
	"ML_SERVICE_URL",
	"ML_SERVICE_TIMEOUT",
	"ACCESS_SERVICE_URL",
	"ACCESS_SERVICE_TIMEOUT",
}

// clearEnv изолирует тест от окружения разработчика: пустое значение = «не задано».
func clearEnv(t *testing.T) {
	t.Helper()

	for _, key := range allEnvKeys {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.App.Env != EnvDev {
		t.Errorf("unexpected APP_ENV: got %q, want %q", cfg.App.Env, EnvDev)
	}

	if cfg.HTTP.Port != defaultPort {
		t.Errorf("unexpected PORT: got %d, want %d", cfg.HTTP.Port, defaultPort)
	}

	if got, want := cfg.HTTP.Addr(), ":8080"; got != want {
		t.Errorf("unexpected addr: got %q, want %q", got, want)
	}

	if cfg.Database.URL != "" || cfg.Redis.Addr != "" {
		t.Errorf("dependencies must be disabled by default: %+v", cfg)
	}

	if cfg.Log.Level != defaultLogLevel {
		t.Errorf("unexpected LOG_LEVEL: got %q, want %q", cfg.Log.Level, defaultLogLevel)
	}

	if cfg.Log.Format != FormatText {
		t.Errorf("unexpected LOG_FORMAT: got %q, want %q", cfg.Log.Format, FormatText)
	}

	if cfg.Log.AddSource {
		t.Error("LOG_ADD_SOURCE must be disabled by default")
	}

	if cfg.LLM.Enabled || cfg.MCP.Enabled {
		t.Errorf("LLM/MCP must be disabled by default: %+v", cfg)
	}

	if cfg.LLM.Timeout != defaultLLMTimeout {
		t.Errorf("unexpected LLM_TIMEOUT: got %s, want %s", cfg.LLM.Timeout, defaultLLMTimeout)
	}

	if cfg.MCP.Timeout != defaultMCPTimeout {
		t.Errorf("unexpected MCP_TIMEOUT: got %s, want %s", cfg.MCP.Timeout, defaultMCPTimeout)
	}

	if cfg.MLService.URL != "" || cfg.AccessService.URL != "" {
		t.Errorf("upstream services must be disabled by default: %+v / %+v", cfg.MLService, cfg.AccessService)
	}

	if cfg.MLService.Timeout != defaultMLTimeout || cfg.AccessService.Timeout != defaultAccessTimeout {
		t.Errorf("unexpected upstream timeouts: %+v / %+v", cfg.MLService, cfg.AccessService)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	clearEnv(t)

	t.Setenv("APP_ENV", "prod")
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://buddy:s3cret@db:5433/buddy?sslmode=disable")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("LOG_ADD_SOURCE", "true")
	t.Setenv("LLM_ENABLED", "true")
	t.Setenv("LLM_BASE_URL", "https://llm.example/v1")
	t.Setenv("LLM_API_KEY", "sk-test")
	t.Setenv("LLM_MODEL", "gpt-test")
	t.Setenv("LLM_TIMEOUT", "45s")
	t.Setenv("MCP_ENABLED", "true")
	t.Setenv("MCP_URL", "https://mcp.example")
	t.Setenv("MCP_TIMEOUT", "5s")
	t.Setenv("ML_SERVICE_URL", "http://ml:9000")
	t.Setenv("ML_SERVICE_TIMEOUT", "12s")
	t.Setenv("ACCESS_SERVICE_URL", "http://access:9001")
	t.Setenv("ACCESS_SERVICE_TIMEOUT", "7s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.App.Env != EnvProd {
		t.Errorf("unexpected APP_ENV: got %q, want %q", cfg.App.Env, EnvProd)
	}

	if cfg.HTTP.Port != 9090 {
		t.Errorf("unexpected PORT: got %d, want 9090", cfg.HTTP.Port)
	}

	if cfg.Database.URL != "postgres://buddy:s3cret@db:5433/buddy?sslmode=disable" {
		t.Errorf("unexpected DATABASE_URL: %q", cfg.Database.URL)
	}

	if cfg.Redis.Addr != "redis:6379" {
		t.Errorf("unexpected REDIS_ADDR: %q", cfg.Redis.Addr)
	}

	if cfg.Log.Level != "debug" || cfg.Log.Format != FormatJSON || !cfg.Log.AddSource {
		t.Errorf("unexpected log config: %+v", cfg.Log)
	}

	if !cfg.LLM.Enabled || cfg.LLM.BaseURL != "https://llm.example/v1" ||
		cfg.LLM.APIKey != "sk-test" || cfg.LLM.Model != "gpt-test" || cfg.LLM.Timeout != 45*time.Second {
		t.Errorf("unexpected LLM config: %+v", cfg.LLM)
	}

	if !cfg.MCP.Enabled || cfg.MCP.URL != "https://mcp.example" || cfg.MCP.Timeout != 5*time.Second {
		t.Errorf("unexpected MCP config: %+v", cfg.MCP)
	}

	if cfg.MLService.URL != "http://ml:9000" || cfg.MLService.Timeout != 12*time.Second {
		t.Errorf("unexpected ML service config: %+v", cfg.MLService)
	}

	if cfg.AccessService.URL != "http://access:9001" || cfg.AccessService.Timeout != 7*time.Second {
		t.Errorf("unexpected access service config: %+v", cfg.AccessService)
	}
}

func TestLoadDerivesLogFormatFromEnv(t *testing.T) {
	tests := []struct {
		name   string
		appEnv string
		format string
		want   string
	}{
		{name: "dev defaults to text", appEnv: EnvDev, format: "", want: FormatText},
		{name: "prod defaults to json", appEnv: EnvProd, format: "", want: FormatJSON},
		{name: "explicit value wins", appEnv: EnvProd, format: FormatText, want: FormatText},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("APP_ENV", test.appEnv)
			if test.format != "" {
				t.Setenv("LOG_FORMAT", test.format)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			if cfg.Log.Format != test.want {
				t.Fatalf("unexpected LOG_FORMAT: got %q, want %q", cfg.Log.Format, test.want)
			}
		})
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "unknown environment",
			env:  map[string]string{"APP_ENV": "staging"},
			want: "APP_ENV",
		},
		{
			name: "port below range",
			env:  map[string]string{"PORT": "0"},
			want: "PORT",
		},
		{
			name: "port above range",
			env:  map[string]string{"PORT": "70000"},
			want: "PORT",
		},
		{
			name: "port is not a number",
			env:  map[string]string{"PORT": "http"},
			want: "PORT",
		},
		{
			name: "invalid database url",
			env:  map[string]string{"DATABASE_URL": "://bad"},
			want: "DATABASE_URL",
		},
		{
			name: "database url without host",
			env:  map[string]string{"DATABASE_URL": "postgres:///buddy"},
			want: "DATABASE_URL",
		},
		{
			name: "database url with wrong scheme",
			env:  map[string]string{"DATABASE_URL": "mysql://buddy:buddy@db/buddy"},
			want: "DATABASE_URL",
		},
		{
			name: "invalid redis addr",
			env:  map[string]string{"REDIS_ADDR": "localhost"},
			want: "REDIS_ADDR",
		},
		{
			name: "redis addr with non-numeric port",
			env:  map[string]string{"REDIS_ADDR": "redis:abc"},
			want: "REDIS_ADDR",
		},
		{
			name: "redis addr with port out of range",
			env:  map[string]string{"REDIS_ADDR": "redis:99999"},
			want: "REDIS_ADDR",
		},
		{
			name: "redis addr without host",
			env:  map[string]string{"REDIS_ADDR": ":6379"},
			want: "REDIS_ADDR",
		},
		{
			name: "database url with port out of range",
			env:  map[string]string{"DATABASE_URL": "postgres://buddy:buddy@db:99999/buddy"},
			want: "DATABASE_URL",
		},
		{
			name: "database without ml service url",
			env: map[string]string{
				"DATABASE_URL":       "postgres://buddy:buddy@db/buddy",
				"ACCESS_SERVICE_URL": "http://access:9001",
			},
			want: "ML_SERVICE_URL",
		},
		{
			name: "database without access service url",
			env: map[string]string{
				"DATABASE_URL":   "postgres://buddy:buddy@db/buddy",
				"ML_SERVICE_URL": "http://ml:9000",
			},
			want: "ACCESS_SERVICE_URL",
		},
		{
			name: "ml service url with unsupported scheme",
			env: map[string]string{
				"DATABASE_URL":       "postgres://buddy:buddy@db/buddy",
				"ML_SERVICE_URL":     "ftp://ml:9000",
				"ACCESS_SERVICE_URL": "http://access:9001",
			},
			want: "ML_SERVICE_URL",
		},
		{
			name: "ml service with non-positive timeout",
			env: map[string]string{
				"DATABASE_URL":       "postgres://buddy:buddy@db/buddy",
				"ML_SERVICE_URL":     "http://ml:9000",
				"ML_SERVICE_TIMEOUT": "0s",
				"ACCESS_SERVICE_URL": "http://access:9001",
			},
			want: "ML_SERVICE_TIMEOUT",
		},
		{
			name: "invalid log level",
			env:  map[string]string{"LOG_LEVEL": "verbose"},
			want: "LOG_LEVEL",
		},
		{
			name: "invalid log format",
			env:  map[string]string{"LOG_FORMAT": "xml"},
			want: "LOG_FORMAT",
		},
		{
			name: "llm without base url",
			env:  map[string]string{"LLM_ENABLED": "true", "LLM_MODEL": "gpt-test"},
			want: "LLM_BASE_URL",
		},
		{
			name: "llm without model",
			env:  map[string]string{"LLM_ENABLED": "true", "LLM_BASE_URL": "https://llm.example/v1"},
			want: "LLM_MODEL",
		},
		{
			name: "llm with non-positive timeout",
			env: map[string]string{
				"LLM_ENABLED":  "true",
				"LLM_BASE_URL": "https://llm.example/v1",
				"LLM_MODEL":    "gpt-test",
				"LLM_TIMEOUT":  "0s",
			},
			want: "LLM_TIMEOUT",
		},
		{
			name: "mcp without url",
			env:  map[string]string{"MCP_ENABLED": "true"},
			want: "MCP_URL",
		},
		{
			name: "mcp with unsupported scheme",
			env:  map[string]string{"MCP_ENABLED": "true", "MCP_URL": "ftp://mcp.example"},
			want: "MCP_URL",
		},
		{
			name: "mcp with non-positive timeout",
			env: map[string]string{
				"MCP_ENABLED": "true",
				"MCP_URL":     "https://mcp.example",
				"MCP_TIMEOUT": "0s",
			},
			want: "MCP_TIMEOUT",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearEnv(t)
			for key, value := range test.env {
				t.Setenv(key, value)
			}

			cfg, err := Load()
			if err == nil {
				t.Fatalf("Load() must fail, got config: %+v", cfg)
			}

			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: got %v, want contains %q", err, test.want)
			}
		})
	}
}

func TestLoadIgnoresForeignEnvironment(t *testing.T) {
	clearEnv(t)

	// Переменные, не относящиеся к конфигу (в том числе совпадающие с именами групп),
	// не должны ломать декодирование.
	for _, key := range []string{"APP", "LLM", "HTTP", "LOG", "MCP", "DATABASE", "REDIS", "PATH", "HOME"} {
		t.Setenv(key, "value")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() must ignore foreign environment variables, got error: %v", err)
	}

	if cfg.HTTP.Port != defaultPort {
		t.Errorf("unexpected PORT: got %d, want %d", cfg.HTTP.Port, defaultPort)
	}
}

func TestLoadIgnoresUnknownConfigKeys(t *testing.T) {
	clearEnv(t)

	// Опечатки в именах не должны ни применяться, ни ронять загрузку.
	t.Setenv("PORTS", "9090")
	t.Setenv("DATBASE_URL", "postgres://buddy:buddy@db:5432/buddy")
	t.Setenv("LOG_FORMT", "json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.HTTP.Port != defaultPort {
		t.Errorf("PORTS must be ignored: got %d, want %d", cfg.HTTP.Port, defaultPort)
	}

	if cfg.Database.URL != "" {
		t.Errorf("DATBASE_URL must be ignored: got %q", cfg.Database.URL)
	}

	if cfg.Log.Format != FormatText {
		t.Errorf("LOG_FORMT must be ignored: got %q, want %q", cfg.Log.Format, FormatText)
	}
}

func TestUnknownConfigEnvVars(t *testing.T) {
	clearEnv(t)

	t.Setenv("LLM_MODLE", "gpt-test")
	t.Setenv("LOG_FORMT", "json")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("POSTGRES_PORT", "5432")

	got := UnknownConfigEnvVars()
	for _, want := range []string{"LLM_MODLE", "LOG_FORMT"} {
		if !contains(got, want) {
			t.Errorf("%q is missing in %v — опечатка останется незамеченной", want, got)
		}
	}

	// Посторонние переменные не должны попадать в предупреждения.
	for _, unexpected := range []string{"PATH", "HOME", "APP", "PORT", "LOG_LEVEL", "REDIS_PORT", "POSTGRES_PORT"} {
		if contains(got, unexpected) {
			t.Errorf("%q must not be reported as unknown, got %v", unexpected, got)
		}
	}
}

func TestLoadTrimsWhitespace(t *testing.T) {
	clearEnv(t)

	t.Setenv("PORT", " 9091 ")
	t.Setenv("APP_ENV", " prod ")
	t.Setenv("LLM_ENABLED", " true ")
	t.Setenv("LLM_BASE_URL", " https://llm.example/v1 ")
	t.Setenv("LLM_MODEL", " gpt-test ")
	t.Setenv("LLM_TIMEOUT", " 5s ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.HTTP.Port != 9091 || cfg.App.Env != EnvProd || cfg.Log.Format != FormatJSON {
		t.Errorf("unexpected config: %+v", cfg)
	}

	if !cfg.LLM.Enabled || cfg.LLM.BaseURL != "https://llm.example/v1" ||
		cfg.LLM.Model != "gpt-test" || cfg.LLM.Timeout != 5*time.Second {
		t.Errorf("unexpected LLM config: %+v", cfg.LLM)
	}
}

func TestSanitizeURLMasksCredentials(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantSecret []string
		wantParts  []string
	}{
		{
			name:       "password in userinfo",
			raw:        "https://user:s3cret@mcp.example/sse",
			wantSecret: []string{"s3cret"},
			wantParts:  []string{"redacted@mcp.example"},
		},
		{
			name:       "token in query",
			raw:        "https://llm.example/v1?api_key=sk-live-1&model=x",
			wantSecret: []string{"sk-live-1"},
			wantParts:  []string{"api_key=redacted", "model=x"},
		},
		{
			name:       "invalid url",
			raw:        "://bad",
			wantSecret: []string{"bad"},
			wantParts:  []string{"***"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeURL(test.raw)

			for _, secret := range test.wantSecret {
				if strings.Contains(got, secret) {
					t.Fatalf("secret %q leaked: %q", secret, got)
				}
			}

			for _, part := range test.wantParts {
				if !strings.Contains(got, part) {
					t.Fatalf("sanitized url %q does not contain %q", got, part)
				}
			}
		})
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func TestLoadReportsEnvNamesForInvalidValues(t *testing.T) {
	clearEnv(t)

	t.Setenv("PORT", "8080abc")
	t.Setenv("LLM_ENABLED", "maybe")
	t.Setenv("LLM_TIMEOUT", "soon")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() must fail on invalid values")
	}

	message := err.Error()
	for _, want := range []string{"PORT", "LLM_ENABLED", "LLM_TIMEOUT", "true or false", "duration"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}

	// Сообщение должно ссылаться на env-переменные, а не на внутренние ключи конфига.
	for _, unexpected := range []string{"llm.enabled", "http.port", "llm.timeout"} {
		if strings.Contains(message, unexpected) {
			t.Errorf("error %q leaks internal key %q", message, unexpected)
		}
	}
}

func TestDatabaseTCPAddr(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{name: "empty", rawURL: "", want: ""},
		{name: "with port", rawURL: "postgres://buddy:buddy@db:5433/buddy?sslmode=disable", want: "db:5433"},
		{name: "without port", rawURL: "postgres://buddy:buddy@db/buddy", want: "db:5432"},
		{name: "invalid url", rawURL: "://bad", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := DatabaseConfig{URL: test.rawURL}
			if got := db.TCPAddr(); got != test.want {
				t.Fatalf("unexpected addr: got %q, want %q", got, test.want)
			}
		})
	}
}

func TestSafeURLMasksPassword(t *testing.T) {
	db := DatabaseConfig{URL: "postgres://buddy:s3cret@db:5432/buddy?sslmode=disable"}

	got := db.SafeURL()
	if strings.Contains(got, "s3cret") {
		t.Fatalf("password leaked: %q", got)
	}

	for _, want := range []string{"redacted@db:5432", "sslmode=disable"} {
		if !strings.Contains(got, want) {
			t.Fatalf("safe url %q does not contain %q", got, want)
		}
	}
}

func TestLogValueRedactsSecrets(t *testing.T) {
	clearEnv(t)

	t.Setenv("DATABASE_URL", "postgres://buddy:s3cret@db:5432/buddy")
	t.Setenv("LLM_ENABLED", "true")
	t.Setenv("LLM_BASE_URL", "https://llm.example/v1?api_key=sk-query-secret")
	t.Setenv("LLM_MODEL", "gpt-test")
	t.Setenv("LLM_API_KEY", "sk-secret")
	t.Setenv("MCP_URL", "https://mcp.example/sse?token=mcp-secret")
	t.Setenv("ML_SERVICE_URL", "http://ml:9000")
	t.Setenv("ACCESS_SERVICE_URL", "http://access:9001")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("config", "config", *cfg)

	out := buf.String()
	for _, secret := range []string{"s3cret", "sk-secret", "sk-query-secret", "mcp-secret"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %q leaked into logs: %s", secret, out)
		}
	}

	for _, want := range []string{`"app_env":"dev"`, `"api_key":"set"`, "redacted"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output %q does not contain %q", out, want)
		}
	}
}
