package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLivenessHandler(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	livenessHandler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", recorder.Code, http.StatusOK)
	}

	if body := recorder.Body.String(); !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestReadinessHandler(t *testing.T) {
	tests := []struct {
		name        string
		deps        []dependency
		wantStatus  int
		wantPayload string
	}{
		{
			name:        "checks are skipped when dependencies are not configured",
			deps:        []dependency{{name: "postgres"}, {name: "redis"}},
			wantStatus:  http.StatusOK,
			wantPayload: `"status":"ok"`,
		},
		{
			name:        "reachable dependency",
			deps:        []dependency{{name: "redis", addr: startTCPServer(t)}},
			wantStatus:  http.StatusOK,
			wantPayload: `"status":"ok"`,
		},
		{
			name:        "unreachable dependency",
			deps:        []dependency{{name: "redis", addr: "127.0.0.1:1"}},
			wantStatus:  http.StatusServiceUnavailable,
			wantPayload: `"status":"degraded"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

			readinessHandler(test.deps).ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("unexpected status code: got %d, want %d", recorder.Code, test.wantStatus)
			}

			if body := recorder.Body.String(); !strings.Contains(body, test.wantPayload) {
				t.Fatalf("unexpected body: %q", body)
			}
		})
	}
}

func TestPostgresAddr(t *testing.T) {
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
			if got := postgresAddr(test.rawURL); got != test.want {
				t.Fatalf("unexpected addr: got %q, want %q", got, test.want)
			}
		})
	}
}

func startTCPServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start tcp listener: %v", err)
	}

	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	return listener.Addr().String()
}
