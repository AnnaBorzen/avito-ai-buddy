// Package logger создаёт настроенный slog-логгер приложения.
package logger

import (
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"os"
)

const (
	// FormatJSON — структурированные JSON-логи, используются в prod.
	FormatJSON = "json"
	// FormatText — human-readable логи, используются в dev.
	FormatText = "text"
)

const defaultLevel = slog.LevelInfo

// Options описывает параметры логгера, полученные из конфига.
type Options struct {
	// Level — текстовое представление уровня (debug, info, warn, error).
	// Пустая строка означает info.
	Level string
	// Format — "json" или "text". Пустая строка означает "text".
	Format string
	// Output — приёмник логов, по умолчанию os.Stdout.
	Output io.Writer
	// AddSource добавляет в записи файл и строку вызова.
	AddSource bool
}

// New собирает slog-логгер с нужным уровнем и форматом.
func New(opts Options) (*slog.Logger, error) {
	output := opts.Output
	if output == nil {
		output = os.Stdout
	}

	level := defaultLevel
	if opts.Level != "" {
		if err := level.UnmarshalText([]byte(opts.Level)); err != nil {
			return nil, fmt.Errorf("logger: invalid level %q: %w", opts.Level, err)
		}
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource,
	}

	var handler slog.Handler
	switch opts.Format {
	case FormatJSON:
		handler = slog.NewJSONHandler(output, handlerOpts)
	case FormatText, "":
		handler = slog.NewTextHandler(output, handlerOpts)
	default:
		return nil, fmt.Errorf("logger: unsupported format %q, want %q or %q", opts.Format, FormatJSON, FormatText)
	}

	return slog.New(handler), nil
}

// SetDefault делает логгер основным для slog и перенаправляет в него стандартный log.
// Без этого библиотеки, использующие log.Printf, пишут мимо slog: без JSON-формата,
// без уровня и без общих полей. Флаги стандартного логгера сбрасываются, иначе
// его timestamp попадёт внутрь поля msg.
func SetDefault(l *slog.Logger) {
	if l == nil {
		return
	}

	slog.SetDefault(l)

	stdlog.SetFlags(0)
	stdlog.SetPrefix("")
	stdlog.SetOutput(slog.NewLogLogger(l.Handler(), slog.LevelInfo).Writer())
}
