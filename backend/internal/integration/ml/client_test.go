package ml

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const proposalJSON = `{
  "reply": "Подготовил черновик заявки.",
  "reply_kind": "proposal",
  "intent": "request_access",
  "intent_confidence": 0.95,
  "sources": [{"doc_id": "doc_01_onboarding_access", "title": "[ТЕСТОВАЯ ИНСТРУКЦИЯ] ...", "section": "s", "excerpt": "e",
               "is_synthetic": true, "source_type": "synthetic_instruction", "source_label": "Тестовая инструкция",
               "source_url": null, "score": 1.0}],
  "slots": {"resource_id": "space_auto_backend_wiki", "access_level": "write", "justification": "Для прохождения чек-листа"},
  "missing_slots": [],
  "expected_slot": null,
  "tool_call": {"name": "propose_access_request",
                "arguments": {"resource_id": "space_auto_backend_wiki", "access_level": "write", "justification": "Для прохождения чек-листа"}},
  "policy": {"allowed": true, "code": "OK", "message": "", "allowed_levels": ["read", "write"]},
  "handoff_suggested": false,
  "handoff_target": "@ivan_petrov",
  "guard": {"injection_detected": false, "flags": []},
  "meta": {"mode": "local", "model": "local-nlu", "retriever": "bm25", "latency_ms": 3.1,
           "scenario_id": "scn_team_space_access", "scenario_version": "2026-11-15.1", "service_version": "1.0.0"},
  "analytics": {"event": "action_proposed", "properties": {"intent": "request_access"}}
}`

func TestTurnSuccess(t *testing.T) {
	var got TurnRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/turn" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Request-Id") != "req-1" {
			t.Fatalf("missing request id header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(proposalJSON))
	}))
	defer srv.Close()

	c := New(srv.URL+"/", time.Second)
	resp, err := c.Turn(context.Background(), TurnRequest{
		RequestID: "req-1",
		User:      UserContext{UserID: "usr_intern_01", Role: "intern", TeamID: "team_auto_backend"},
		Message:   "Оформи доступ",
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if got.User.UserID != "usr_intern_01" || got.Message != "Оформи доступ" {
		t.Fatalf("request not forwarded: %+v", got)
	}
	if resp.ReplyKind != "proposal" || resp.ToolCall == nil || resp.ToolCall.Arguments.AccessLevel != "write" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !resp.Sources[0].IsSynthetic || resp.Policy == nil || !resp.Policy.Allowed || resp.Meta.Mode != "local" {
		t.Fatalf("unexpected fields: %+v", resp)
	}
}

func TestTurnErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"server error", http.StatusInternalServerError, `{}`, ErrUnavailable},
		{"bad request", http.StatusBadRequest, `{"error":{"code":"VALIDATION_ERROR"}}`, ErrBadRequest},
		{"malformed json", http.StatusOK, `{not json`, ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := New(srv.URL, time.Second).Turn(context.Background(), TurnRequest{Message: "x"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestTurnTimeoutAndUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, 20*time.Millisecond).Turn(context.Background(), TurnRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want timeout ErrUnavailable, got %v", err)
	}
	if _, err := New("http://127.0.0.1:1", time.Second).Turn(context.Background(), TurnRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if _, err := New("://bad", time.Second).Turn(context.Background(), TurnRequest{}); err == nil {
		t.Fatalf("want build error")
	}
}

func TestReady(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ok.Close()
	if err := New(ok.URL, time.Second).Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	if err := New(bad.URL, time.Second).Ready(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if err := New("http://127.0.0.1:1", time.Second).Ready(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if err := New("://bad", time.Second).Ready(context.Background()); err == nil {
		t.Fatalf("want build error")
	}
}
