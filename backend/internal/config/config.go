package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
)

const (
	// EnvDev — окружение разработки: текстовые логи и подробный вывод.
	EnvDev = "dev"
	// EnvProd — продакшн-окружение: структурированные JSON-логи.
	EnvProd = "prod"

	// FormatJSON — формат логов json.
	FormatJSON = "json"
	// FormatText — формат логов text.
	FormatText = "text"

	defaultPort       = 8080
	defaultLogLevel   = "info"
	defaultLLMTimeout = 30 * time.Second
	defaultMCPTimeout = 10 * time.Second
)

// Config — полный набор настроек сервиса.
type Config struct {
	App      AppConfig      `koanf:"app"`
	HTTP     HTTPConfig     `koanf:"http"`
	Database DatabaseConfig `koanf:"database"`
	Redis    RedisConfig    `koanf:"redis"`
	Log      LogConfig      `koanf:"log"`
	LLM      LLMConfig      `koanf:"llm"`
	MCP      MCPConfig      `koanf:"mcp"`
}

// AppConfig — общие параметры приложения.
type AppConfig struct {
	// Env задаётся через APP_ENV: dev или prod.
	Env string `koanf:"env"`
}

// HTTPConfig — параметры HTTP-сервера.
type HTTPConfig struct {
	// Port задаётся через PORT.
	Port int `koanf:"port"`
}

// Addr возвращает адрес прослушивания для http.Server.
func (h HTTPConfig) Addr() string {
	return fmt.Sprintf(":%d", h.Port)
}

// DatabaseConfig — подключение к PostgreSQL.
type DatabaseConfig struct {
	// URL задаётся через DATABASE_URL; пустое значение отключает проверку в /readyz.
	URL string `koanf:"url"`
}

// RedisConfig — подключение к Redis.
type RedisConfig struct {
	// Addr задаётся через REDIS_ADDR; пустое значение отключает проверку в /readyz.
	Addr string `koanf:"addr"`
}

// LogConfig — настройки логгера.
type LogConfig struct {
	// Level задаётся через LOG_LEVEL.
	Level string `koanf:"level"`
	// Format задаётся через LOG_FORMAT; по умолчанию выводится из APP_ENV.
	Format string `koanf:"format"`
	// AddSource задаётся через LOG_ADD_SOURCE и добавляет в записи файл и строку вызова.
	AddSource bool `koanf:"add_source"`
}

// LLMConfig — флаги и параметры интеграции с LLM.
type LLMConfig struct {
	Enabled bool          `koanf:"enabled"`
	BaseURL string        `koanf:"base_url"`
	APIKey  string        `koanf:"api_key"`
	Model   string        `koanf:"model"`
	Timeout time.Duration `koanf:"timeout"`
}

// MCPConfig — флаги и параметры интеграции с MCP.
type MCPConfig struct {
	Enabled bool          `koanf:"enabled"`
	URL     string        `koanf:"url"`
	Timeout time.Duration `koanf:"timeout"`
}

// envKind задаёт тип значения, которое приходит из окружения.
type envKind int

const (
	envKindString envKind = iota
	envKindBool
	envKindInt
	envKindDuration
)

// envSpec описывает переменную окружения: ключ конфига и тип значения.
type envSpec struct {
	key  string
	kind envKind
}

// envKeys — единственный источник правды о переменных, которые читает сервис.
// Всё, чего здесь нет, игнорируется: посторонние переменные процесса (APP, PATH,
// HTTP_PROXY) не должны попадать в koanf и ронять запуск.
var envKeys = map[string]envSpec{
	"APP_ENV":        {key: "app.env"},
	"PORT":           {key: "http.port", kind: envKindInt},
	"DATABASE_URL":   {key: "database.url"},
	"REDIS_ADDR":     {key: "redis.addr"},
	"LOG_LEVEL":      {key: "log.level"},
	"LOG_FORMAT":     {key: "log.format"},
	"LOG_ADD_SOURCE": {key: "log.add_source", kind: envKindBool},
	"LLM_ENABLED":    {key: "llm.enabled", kind: envKindBool},
	"LLM_BASE_URL":   {key: "llm.base_url"},
	"LLM_API_KEY":    {key: "llm.api_key"},
	"LLM_MODEL":      {key: "llm.model"},
	"LLM_TIMEOUT":    {key: "llm.timeout", kind: envKindDuration},
	"MCP_ENABLED":    {key: "mcp.enabled", kind: envKindBool},
	"MCP_URL":        {key: "mcp.url"},
	"MCP_TIMEOUT":    {key: "mcp.timeout", kind: envKindDuration},
}

// knownPrefixes — префиксы переменных, которые принадлежат сервису.
// Нужны только для предупреждения об опечатках (LLM_MODLE, LOG_FORMT).
var knownPrefixes = []string{"APP_", "HTTP_", "DATABASE_", "REDIS_", "LOG_", "LLM_", "MCP_"}

// foreignEnvKeys — переменные docker-compose и CI, которые сервис осознанно не читает.
// Без этого списка предупреждение об опечатках срабатывало бы на каждом `make backend`.
var foreignEnvKeys = map[string]struct{}{
	"BACKEND_PORT":      {},
	"FRONTEND_PORT":     {},
	"POSTGRES_USER":     {},
	"POSTGRES_PASSWORD": {},
	"POSTGRES_DB":       {},
	"POSTGRES_PORT":     {},
	"REDIS_PORT":        {},
}

// parseEnvValue приводит значение переменной к типу из envSpec.
// Ошибка называет переменную окружения, а не внутренний ключ конфига:
// сообщение про 'llm.enabled' непонятно тому, кто правит .env.
func parseEnvValue(name string, spec envSpec, raw string) (any, error) {
	switch spec.kind {
	case envKindBool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: want true or false, got %q", name, raw)
		}

		return value, nil
	case envKindInt:
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: want integer, got %q", name, raw)
		}

		return value, nil
	case envKindDuration:
		value, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: want duration like 5s or 1m, got %q", name, raw)
		}

		return value, nil
	case envKindString:
		return raw, nil
	}

	return raw, nil
}

// UnknownConfigEnvVars возвращает переменные с известным сервису префиксом,
// которые конфиг не читает. Так опечатки (LLM_MODLE, DATBASE_URL, LOG_FORMT)
// не остаются незамеченными и попадают в предупреждение при старте.
func UnknownConfigEnvVars() []string {
	var unknown []string

	for _, raw := range os.Environ() {
		name, _, _ := strings.Cut(raw, "=")
		upper := strings.ToUpper(name)

		if _, known := envKeys[upper]; known {
			continue
		}

		if _, foreign := foreignEnvKeys[upper]; foreign {
			continue
		}

		if !hasKnownPrefix(upper) {
			continue
		}

		unknown = append(unknown, upper)
	}

	sort.Strings(unknown)

	return unknown
}

func hasKnownPrefix(name string) bool {
	for _, prefix := range knownPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// defaults повторяет значения из .env.example и позволяет запускать сервис без окружения.
var defaults = map[string]any{
	"app.env":        EnvDev,
	"http.port":      defaultPort,
	"database.url":   "",
	"redis.addr":     "",
	"log.level":      defaultLogLevel,
	"log.format":     "",
	"log.add_source": false,
	"llm.enabled":    false,
	"llm.base_url":   "",
	"llm.api_key":    "",
	"llm.model":      "",
	"llm.timeout":    defaultLLMTimeout,
	"mcp.enabled":    false,
	"mcp.url":        "",
	"mcp.timeout":    defaultMCPTimeout,
}

// Load читает переменные окружения поверх дефолтов и валидирует результат.
// Пустые значения считаются незаданными, чтобы `VAR=` не затирал дефолт.
func Load() (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return nil, fmt.Errorf("config: load defaults: %w", err)
	}

	var invalid []error

	provider := env.ProviderWithValue("", ".", func(name, value string) (string, any) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", nil
		}

		key := strings.ToUpper(strings.TrimSpace(name))
		spec, known := envKeys[key]
		if !known {
			// Посторонние переменные и опечатки не попадают в конфиг.
			return "", nil
		}

		parsed, err := parseEnvValue(key, spec, trimmed)
		if err != nil {
			invalid = append(invalid, err)
			return "", nil
		}

		return spec.key, parsed
	})
	if err := k.Load(provider, nil); err != nil {
		return nil, fmt.Errorf("config: load environment: %w", err)
	}

	if len(invalid) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(invalid...))
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: decode: %w", err)
	}

	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// normalize приводит значения к каноническому виду и подставляет зависимые дефолты.
func (c *Config) normalize() {
	c.App.Env = strings.ToLower(strings.TrimSpace(c.App.Env))
	if c.App.Env == "" {
		c.App.Env = EnvDev
	}

	c.Log.Level = strings.ToLower(strings.TrimSpace(c.Log.Level))
	if c.Log.Level == "" {
		c.Log.Level = defaultLogLevel
	}

	c.Log.Format = strings.ToLower(strings.TrimSpace(c.Log.Format))
	if c.Log.Format == "" {
		c.Log.Format = FormatText
		if c.App.Env == EnvProd {
			c.Log.Format = FormatJSON
		}
	}
}

// Validate проверяет конфиг и возвращает сразу все найденные проблемы.
func (c *Config) Validate() error {
	var problems []error

	switch c.App.Env {
	case EnvDev, EnvProd:
	default:
		problems = append(problems, fmt.Errorf("APP_ENV: want %q or %q, got %q", EnvDev, EnvProd, c.App.Env))
	}

	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		problems = append(problems, fmt.Errorf("PORT: want 1..65535, got %d", c.HTTP.Port))
	}

	if c.Database.URL != "" {
		if err := validatePostgresURL(c.Database.URL); err != nil {
			problems = append(problems, err)
		}
	}

	if c.Redis.Addr != "" {
		if err := validateRedisAddr(c.Redis.Addr); err != nil {
			problems = append(problems, err)
		}
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Log.Level)); err != nil {
		problems = append(problems, fmt.Errorf("LOG_LEVEL: unsupported value %q", c.Log.Level))
	}

	switch c.Log.Format {
	case FormatJSON, FormatText:
	default:
		problems = append(problems, fmt.Errorf("LOG_FORMAT: want %q or %q, got %q", FormatJSON, FormatText, c.Log.Format))
	}

	if c.LLM.Enabled {
		if err := validateHTTPURL("LLM_BASE_URL", c.LLM.BaseURL); err != nil {
			problems = append(problems, err)
		}
		if strings.TrimSpace(c.LLM.Model) == "" {
			problems = append(problems, errors.New("LLM_MODEL: required when LLM_ENABLED=true"))
		}
		if c.LLM.Timeout <= 0 {
			problems = append(problems, fmt.Errorf("LLM_TIMEOUT: must be positive, got %s", c.LLM.Timeout))
		}
	}

	if c.MCP.Enabled {
		if err := validateHTTPURL("MCP_URL", c.MCP.URL); err != nil {
			problems = append(problems, err)
		}
		if c.MCP.Timeout <= 0 {
			problems = append(problems, fmt.Errorf("MCP_TIMEOUT: must be positive, got %s", c.MCP.Timeout))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("config: %w", errors.Join(problems...))
	}

	return nil
}

// validatePostgresURL намеренно не оборачивает ошибку url.Parse:
// её текст содержит исходный DSN вместе с паролем.
func validatePostgresURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.New("DATABASE_URL: must be a valid postgres connection URL")
	}

	switch parsed.Scheme {
	case "postgres", "postgresql":
	default:
		return errors.New(`DATABASE_URL: scheme must be "postgres" or "postgresql"`)
	}

	if parsed.Host == "" {
		return errors.New("DATABASE_URL: host is required")
	}

	if port := parsed.Port(); port != "" {
		number, convErr := strconv.Atoi(port)
		if convErr != nil || number < 1 || number > 65535 {
			return fmt.Errorf("DATABASE_URL: invalid port %q, want 1..65535", port)
		}
	}

	return nil
}

// validateRedisAddr требует host:port: сам SplitHostPort принимает "redis:abc"
// и ":6379", поэтому порт и хост проверяются отдельно.
func validateRedisAddr(raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" {
		return fmt.Errorf("REDIS_ADDR: want host:port, got %q", raw)
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("REDIS_ADDR: invalid port %q, want 1..65535", port)
	}

	return nil
}

func validateHTTPURL(name, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s: required", name)
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%s: must be a valid URL", name)
	}

	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf(`%s: scheme must be "http" or "https"`, name)
	}

	return nil
}

// TCPAddr извлекает host:port из DATABASE_URL для readiness-проверки.
// Пустая строка означает, что проверка не настроена.
func (d DatabaseConfig) TCPAddr() string {
	if d.URL == "" {
		return ""
	}

	parsed, err := url.Parse(d.URL)
	if err != nil || parsed.Host == "" {
		return ""
	}

	if parsed.Port() == "" {
		return net.JoinHostPort(parsed.Hostname(), "5432")
	}

	return parsed.Host
}

// SafeURL возвращает DSN с замаскированными секретами — для логов.
func (d DatabaseConfig) SafeURL() string {
	return sanitizeURL(d.URL)
}

// sensitiveQueryMarkers — подстроки имён query-параметров, значения которых
// нельзя показывать в логах: api_key, access_token, client_secret, signature.
var sensitiveQueryMarkers = []string{"key", "token", "secret", "password", "pass", "auth", "credential", "sig"}

// sanitizeURL маскирует пароль в userinfo и значения секретных query-параметров.
// Используется только для логов, поэтому неразборчивый URL заменяется на "***".
func sanitizeURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "***"
	}

	if _, hasPassword := parsed.User.Password(); hasPassword {
		parsed.User = url.UserPassword(parsed.User.Username(), "redacted")
	}

	if parsed.RawQuery != "" {
		query := parsed.Query()
		masked := false

		for name := range query {
			if isSensitiveQueryName(name) {
				query.Set(name, "redacted")
				masked = true
			}
		}

		if masked {
			parsed.RawQuery = query.Encode()
		}
	}

	return parsed.String()
}

func isSensitiveQueryName(name string) bool {
	lower := strings.ToLower(name)

	for _, marker := range sensitiveQueryMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

// LogValue реализует slog.LogValuer и не даёт секретам попасть в логи.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("app_env", c.App.Env),
		slog.Group("http", slog.Int("port", c.HTTP.Port)),
		slog.Group("database",
			slog.Bool("configured", c.Database.URL != ""),
			slog.String("url", c.Database.SafeURL()),
		),
		slog.Group("redis",
			slog.Bool("configured", c.Redis.Addr != ""),
			slog.String("addr", c.Redis.Addr),
		),
		slog.Group("log",
			slog.String("level", c.Log.Level),
			slog.String("format", c.Log.Format),
			slog.Bool("add_source", c.Log.AddSource),
		),
		slog.Group("llm",
			slog.Bool("enabled", c.LLM.Enabled),
			slog.String("base_url", sanitizeURL(c.LLM.BaseURL)),
			slog.String("model", c.LLM.Model),
			slog.String("api_key", credentialState(c.LLM.APIKey)),
			slog.String("timeout", c.LLM.Timeout.String()),
		),
		slog.Group("mcp",
			slog.Bool("enabled", c.MCP.Enabled),
			slog.String("url", sanitizeURL(c.MCP.URL)),
			slog.String("timeout", c.MCP.Timeout.String()),
		),
	)
}

func credentialState(secret string) string {
	if secret == "" {
		return "unset"
	}

	return "set"
}
