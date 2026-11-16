package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/access"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/ml"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/service"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testRouter(t *testing.T) *Router {
	t.Helper()
	seed, err := domain.NewSeed()
	if err != nil {
		t.Fatalf("domain.NewSeed() error: %v", err)
	}
	return NewRouter(nil, seed, testLogger())
}

func serve(t *testing.T, router *Router, req *nethttp.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := nethttp.NewServeMux()
	router.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func TestTestProfilesReturnsSeedOnly(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/api/v1/test-profiles", nil)
	rec := serve(t, testRouter(t), req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, nethttp.StatusOK, rec.Body)
	}
	var body struct {
		Profiles []struct {
			UserID   string `json:"user_id"`
			TeamName string `json:"team_name"`
		} `json:"profiles"`
	}
	decodeResponse(t, rec, &body)
	if len(body.Profiles) != len(domain.SeedProfiles()) {
		t.Fatalf("profiles = %d, want %d", len(body.Profiles), len(domain.SeedProfiles()))
	}
	for _, profile := range body.Profiles {
		if profile.UserID == "" || profile.TeamName == "" {
			t.Fatalf("profile must expose user_id and team_name: %+v", profile)
		}
	}
}

func TestUnknownTestUserIsRejected(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodPost, "/api/v1/chat/messages", strings.NewReader(`{}`))
	req.Header.Set(testUserHeader, "usr_missing")
	rec := serve(t, testRouter(t), req)

	if rec.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, nethttp.StatusUnauthorized, rec.Body)
	}
	var body apiError
	decodeResponse(t, rec, &body)
	if body.ErrorCode != "USER_NOT_FOUND" {
		t.Fatalf("error_code = %q, want USER_NOT_FOUND", body.ErrorCode)
	}
}

func TestChatRejectsInvalidBodyBeforeService(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing task", body: `{}`, want: "task_instance_id"},
		{name: "unknown field", body: `{"task_instance_id":"tsk_901","role":"lead"}`, want: "body"},
		{name: "trailing object", body: `{"task_instance_id":"tsk_901"}{}`, want: "body"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(nethttp.MethodPost, "/api/v1/chat/messages", strings.NewReader(test.body))
			rec := serve(t, testRouter(t), req)

			if rec.Code != nethttp.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, nethttp.StatusBadRequest, rec.Body)
			}
			var body struct {
				ErrorCode string         `json:"error_code"`
				Details   map[string]any `json:"details"`
			}
			decodeResponse(t, rec, &body)
			if body.ErrorCode != "VALIDATION_FAILED" {
				t.Fatalf("error_code = %q, want VALIDATION_FAILED", body.ErrorCode)
			}
			if body.Details["field"] != test.want {
				t.Fatalf("details.field = %v, want %q", body.Details["field"], test.want)
			}
		})
	}
}

func TestConfirmRejectsInvalidKeyAndOperationBeforeService(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		key    string
		body   string
		status int
		field  string
	}{
		{
			name: "invalid operation id", path: "/api/v1/operations/bogus/confirm",
			key: "idem-0001", body: `{"draft_version":1}`, status: nethttp.StatusBadRequest, field: "operation_id",
		},
		{
			name: "missing idempotency key", path: "/api/v1/operations/op_1/confirm",
			body: `{"draft_version":1}`, status: nethttp.StatusBadRequest, field: "idempotency_key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(nethttp.MethodPost, test.path, strings.NewReader(test.body))
			if test.key != "" {
				req.Header.Set("Idempotency-Key", test.key)
			}
			rec := serve(t, testRouter(t), req)

			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, test.status, rec.Body)
			}
			var body struct {
				Details map[string]any `json:"details"`
			}
			decodeResponse(t, rec, &body)
			if body.Details["field"] != test.field {
				t.Fatalf("details.field = %v, want %q", body.Details["field"], test.field)
			}
		})
	}
}

func TestMapErrorCatalog(t *testing.T) {
	upstream := func(kind, cause error) *service.UpstreamError {
		return &service.UpstreamError{
			Kind: kind, Operation: "op_42", Status: domain.StatusUnknown, Cause: cause,
		}
	}

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name: "validation", err: &domain.ValidationError{Field: "message", Message: "is required"},
			wantStatus: nethttp.StatusBadRequest, wantCode: "VALIDATION_FAILED",
		},
		{
			name: "draft conflict", err: &domain.DraftVersionConflictError{Expected: 1, Actual: 2},
			wantStatus: nethttp.StatusConflict, wantCode: "DRAFT_VERSION_CONFLICT",
		},
		{
			name: "rbac", err: &domain.AccessDeniedError{
				UserID: "usr_intern_01", Role: domain.RoleIntern,
				ResourceID: "space_goods_search_wiki", AccessLevel: domain.AccessWrite,
			},
			wantStatus: nethttp.StatusForbidden, wantCode: "RBAC_ACCESS_DENIED",
		},
		{
			name: "raw forbidden", err: domain.ErrForbidden,
			wantStatus: nethttp.StatusForbidden, wantCode: "RBAC_ACCESS_DENIED",
		},
		{
			name: "missing operation", err: service.ErrTaskNotFound,
			wantStatus: nethttp.StatusNotFound, wantCode: "OPERATION_NOT_FOUND",
		},
		{
			name: "upstream timeout", err: upstream(service.ErrUpstreamTimeout, access.ErrTimeout),
			wantStatus: nethttp.StatusGatewayTimeout, wantCode: "UPSTREAM_TIMEOUT",
		},
		{
			name: "upstream unavailable", err: upstream(service.ErrUpstreamError, access.ErrUnavailable),
			wantStatus: nethttp.StatusServiceUnavailable, wantCode: "UPSTREAM_ERROR",
		},
		{
			name: "upstream rejected", err: upstream(service.ErrUpstreamError, access.ErrRejected),
			wantStatus: nethttp.StatusBadGateway, wantCode: "UPSTREAM_ERROR",
		},
		{
			name: "ml bad request", err: ml.ErrBadRequest,
			wantStatus: nethttp.StatusBadGateway, wantCode: "UPSTREAM_ERROR",
		},
		{
			name: "unknown error", err: errors.New("boom"),
			wantStatus: nethttp.StatusInternalServerError, wantCode: "INTERNAL_ERROR",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, payload := mapError(testLogger(), test.err)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
			if payload.ErrorCode != test.wantCode {
				t.Fatalf("error_code = %q, want %q", payload.ErrorCode, test.wantCode)
			}
			if strings.Contains(payload.Message, "boom") {
				t.Fatalf("internal error leaked to the client: %q", payload.Message)
			}
		})
	}
}

func TestUpstreamErrorCarriesOperationState(t *testing.T) {
	_, payload := mapError(testLogger(), &service.UpstreamError{
		Kind: service.ErrUpstreamTimeout, Operation: "op_42",
		Status: domain.StatusUnknown, Cause: access.ErrTimeout,
	})
	if payload.OperationID != "op_42" || payload.OperationStatus != string(domain.StatusUnknown) {
		t.Fatalf("operation state lost: %+v", payload)
	}
}
