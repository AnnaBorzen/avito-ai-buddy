// Package accessservice implements the separate access-service mock defined by
// contracts/access-service.openapi.yaml. It owns its tickets and has no access
// to backend operations except through the documented HTTP contract.
package accessservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	// simulatedTimeout keeps a demo "timeout" request open longer than the
	// backend's 10s access-service timeout, without leaking forever.
	simulatedTimeout = 30 * time.Second
)

var (
	operationIDPattern = regexp.MustCompile(`^op_[0-9]{1,10}$`)
	// supportedResources is the enum of CreateAccessRequest in
	// contracts/access-service.openapi.yaml.
	supportedResources = map[string]struct{}{
		"space_auto_backend_wiki": {},
		"repo_auto_core_sandbox":  {},
		"space_goods_search_wiki": {},
		"db_fintech_prod_cluster": {},
	}
)

// Server serves the access-service HTTP contract.
type Server struct {
	store *store.Store
	log   *slog.Logger
	http  *http.Server
}

func New(addr string, st *store.Store, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{store: st, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/access-requests", s.create)
	mux.HandleFunc("GET /internal/access-requests", s.lookup)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", s.ready)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
	return s
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("access service listen: %w", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(listener) }()
	s.log.Info("access service started", "addr", listener.Addr().String())

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("access service: %w", err)
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("access service shutdown: %w", err)
	}
	return nil
}

type createRequest struct {
	OperationID   string `json:"operation_id"`
	UserID        string `json:"user_id"`
	TeamID        string `json:"team_id"`
	ResourceID    string `json:"resource_id"`
	AccessLevel   string `json:"access_level"`
	Justification string `json:"justification"`
	ApproverID    string `json:"approver_id"`
	ApproverName  string `json:"approver_name"`
}

type ticketResponse struct {
	TicketID       string    `json:"ticket_id"`
	OperationID    string    `json:"operation_id"`
	ExternalStatus string    `json:"external_status"`
	CreatedAt      time.Time `json:"created_at"`
	ResourceID     string    `json:"resource_id"`
	AccessLevel    string    `json:"access_level"`
	ApproverName   string    `json:"approver_name"`
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if n := utf8.RuneCountInString(key); n < 8 || n > 128 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"Idempotency-Key must contain 8..128 characters.")
		return
	}
	// X-Simulate-Mode is the documented demo switch of the mock. It is
	// evaluated before parsing the body so a simulated outage never writes.
	switch mode := strings.TrimSpace(r.Header.Get("X-Simulate-Mode")); mode {
	case "", "normal":
	case "service_error":
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"Simulated access-service failure.")
		return
	case "timeout":
		select {
		case <-r.Context().Done():
		case <-time.After(simulatedTimeout):
			writeError(w, http.StatusGatewayTimeout, "SERVICE_UNAVAILABLE",
				"Simulated access-service timeout.")
		}
		return
	default:
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"X-Simulate-Mode must be normal, timeout or service_error.")
		return
	}
	var req createRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16_000))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"Request body must be a valid JSON object.")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"Request body must contain exactly one JSON object.")
		return
	}
	if msg := validateCreate(req); msg != "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", msg)
		return
	}

	ticket, created, err := s.store.CreateAccessTicket(r.Context(), store.AccessTicket{
		OperationID: req.OperationID, UserID: req.UserID, TeamID: req.TeamID,
		ResourceID: req.ResourceID, AccessLevel: req.AccessLevel,
		Justification: strings.TrimSpace(req.Justification), ApproverID: req.ApproverID,
		ApproverName: req.ApproverName,
	})
	if errors.Is(err, store.ErrTicketConflict) {
		writeError(w, http.StatusConflict, "VALIDATION_FAILED",
			"operation_id is already bound to another ticket.")
		return
	}
	if err != nil {
		s.log.Error("create access ticket", "error", err)
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"Service could not persist the request.")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toTicketResponse(ticket))
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	operationID := r.URL.Query().Get("operation_id")
	if !operationIDPattern.MatchString(operationID) {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"operation_id must match ^op_[0-9]{1,10}$.")
		return
	}
	ticket, err := s.store.FindAccessTicket(r.Context(), operationID)
	if err != nil {
		s.log.Error("lookup access ticket", "error", err)
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Service unavailable.")
		return
	}
	if ticket == nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "ticket": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "ticket": toTicketResponse(*ticket)})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.AccessReady(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable", "error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func validateCreate(req createRequest) string {
	switch {
	case !operationIDPattern.MatchString(req.OperationID):
		return "operation_id must match ^op_[0-9]{1,10}$."
	case strings.TrimSpace(req.UserID) == "":
		return "user_id is required."
	case strings.TrimSpace(req.TeamID) == "":
		return "team_id is required."
	case strings.TrimSpace(req.ResourceID) == "":
		return "resource_id is required."
	case !isSupportedResource(req.ResourceID):
		return "resource_id is not supported."
	case req.AccessLevel != "read" && req.AccessLevel != "write":
		return "access_level must be read or write."
	case strings.TrimSpace(req.ApproverID) == "":
		return "approver_id is required."
	case strings.TrimSpace(req.ApproverName) == "":
		return "approver_name is required."
	}
	n := utf8.RuneCountInString(strings.TrimSpace(req.Justification))
	if n < 10 || n > 300 {
		return "justification length must be between 10 and 300 characters."
	}
	return ""
}

func isSupportedResource(resourceID string) bool {
	_, ok := supportedResources[resourceID]
	return ok
}

func toTicketResponse(ticket store.AccessTicket) ticketResponse {
	return ticketResponse{
		TicketID: ticket.TicketID, OperationID: ticket.OperationID,
		ExternalStatus: ticket.ExternalStatus, CreatedAt: ticket.CreatedAt,
		ResourceID: ticket.ResourceID, AccessLevel: ticket.AccessLevel,
		ApproverName: ticket.ApproverName,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error_code": code, "message": message})
}
