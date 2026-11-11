package logger

import (
	"bytes"
	"encoding/json"
	stdlog "log"
	"log/slog"
	"strings"
	"testing"
)

func TestSetDefaultBridgesStandardLog(t *testing.T) {
	var buf bytes.Buffer

	log, err := New(Options{Format: FormatJSON, Output: &buf})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	previousDefault := slog.Default()
	previousFlags, previousPrefix, previousOutput := stdlog.Flags(), stdlog.Prefix(), stdlog.Writer()
	t.Cleanup(func() {
		slog.SetDefault(previousDefault)
		stdlog.SetFlags(previousFlags)
		stdlog.SetPrefix(previousPrefix)
		stdlog.SetOutput(previousOutput)
	})

	SetDefault(log)

	if slog.Default() != log {
		t.Fatal("SetDefault must install the given logger as slog default")
	}

	stdlog.Print("legacy message")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("standard log record is not valid json: %v (output %q)", err, buf.String())
	}

	if entry["msg"] != "legacy message" || entry["level"] != "INFO" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
}

func TestSetDefaultIgnoresNil(t *testing.T) {
	previousDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousDefault) })

	SetDefault(nil)

	if slog.Default() != previousDefault {
		t.Fatal("SetDefault(nil) must not change slog default")
	}
}

func TestNewJSONFormat(t *testing.T) {
	var buf bytes.Buffer

	log, err := New(Options{Level: "info", Format: FormatJSON, Output: &buf})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	log.Info("hello", "key", "value")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log entry is not valid json: %v (output %q)", err, buf.String())
	}

	if entry["level"] != "INFO" || entry["msg"] != "hello" || entry["key"] != "value" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
}

func TestNewTextFormat(t *testing.T) {
	var buf bytes.Buffer

	log, err := New(Options{Format: FormatText, Output: &buf})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	log.Info("hello", "key", "value")

	out := buf.String()
	for _, want := range []string{"level=INFO", "msg=hello", "key=value"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestNewDefaultFormatIsText(t *testing.T) {
	var buf bytes.Buffer

	log, err := New(Options{Output: &buf})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	log.Info("plain")

	if out := strings.TrimSpace(buf.String()); strings.HasPrefix(out, "{") {
		t.Fatalf("expected text output, got %q", out)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer

	log, err := New(Options{Level: "warn", Format: FormatJSON, Output: &buf})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	log.Debug("debug message")
	log.Info("info message")
	log.Warn("warn message")

	out := buf.String()
	if strings.Contains(out, "debug message") || strings.Contains(out, "info message") {
		t.Fatalf("entries below warn level must be filtered out, got %q", out)
	}

	if !strings.Contains(out, "warn message") {
		t.Fatalf("warn entry is missing, got %q", out)
	}
}

func TestNewInvalidOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{name: "invalid level", opts: Options{Level: "verbose"}, wantErr: "invalid level"},
		{name: "invalid format", opts: Options{Format: "xml"}, wantErr: "unsupported format"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.opts); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("unexpected error: got %v, want contains %q", err, test.wantErr)
			}
		})
	}
}
