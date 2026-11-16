package http

import (
	"log/slog"
	nethttp "net/http"
	"strings"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/service"
)

const (
	defaultTestUser = "usr_intern_01"
	testUserHeader  = "X-Test-User-Id"
)

// Router mounts the public API on a mux. It owns only HTTP concerns: user
// identity from the demo header, request decoding and error mapping.
type Router struct {
	service *service.Service
	seed    domain.Seed
	log     *slog.Logger
}

func NewRouter(svc *service.Service, seed domain.Seed, log *slog.Logger) *Router {
	if log == nil {
		log = slog.Default()
	}
	return &Router{service: svc, seed: seed, log: log}
}

// Register adds every /api/v1 route of contracts/api.yaml.
func (r *Router) Register(mux *nethttp.ServeMux) {
	mux.HandleFunc("GET /api/v1/context", r.context)
	mux.HandleFunc("POST /api/v1/chat/messages", r.chat)
	mux.HandleFunc("POST /api/v1/operations/{operation_id}/confirm", r.confirm)
	mux.HandleFunc("POST /api/v1/operations/{operation_id}/reconcile", r.reconcile)
	mux.HandleFunc("GET /api/v1/test-profiles", r.testProfiles)
}

func (r *Router) context(w nethttp.ResponseWriter, req *nethttp.Request) {
	userID, ok := r.user(w, req)
	if !ok {
		return
	}
	out, err := r.service.Context(req.Context(), userID)
	if err != nil {
		respondError(w, r.log, err)
		return
	}
	writeJSON(r.log, w, nethttp.StatusOK, out)
}

func (r *Router) chat(w nethttp.ResponseWriter, req *nethttp.Request) {
	userID, ok := r.user(w, req)
	if !ok {
		return
	}
	var body struct {
		TaskInstanceID   string  `json:"task_instance_id"`
		Message          string  `json:"message"`
		HandoffRequested bool    `json:"handoff_requested"`
		SimulateMode     *string `json:"simulate_mode"`
	}
	if err := decodeJSON(w, req, &body); err != nil {
		respondError(w, r.log, err)
		return
	}
	if strings.TrimSpace(body.TaskInstanceID) == "" {
		respondError(w, r.log, &domain.ValidationError{Field: "task_instance_id", Message: "is required"})
		return
	}
	out, err := r.service.Chat(req.Context(), userID, service.ChatRequest{
		TaskInstanceID:   domain.TaskInstanceID(body.TaskInstanceID),
		Message:          body.Message,
		HandoffRequested: body.HandoffRequested,
		SimulateMode:     body.SimulateMode,
	})
	if err != nil {
		respondError(w, r.log, err)
		return
	}
	writeJSON(r.log, w, nethttp.StatusOK, out)
}

func (r *Router) confirm(w nethttp.ResponseWriter, req *nethttp.Request) {
	userID, ok := r.user(w, req)
	if !ok {
		return
	}
	operationID, ok := r.operationID(w, req)
	if !ok {
		return
	}
	key := domain.IdempotencyKey(strings.TrimSpace(req.Header.Get("Idempotency-Key")))
	if err := key.Validate(); err != nil {
		respondError(w, r.log, err)
		return
	}
	var body struct {
		DraftVersion int     `json:"draft_version"`
		SimulateMode *string `json:"simulate_mode"`
	}
	if err := decodeJSON(w, req, &body); err != nil {
		respondError(w, r.log, err)
		return
	}
	out, err := r.service.Confirm(req.Context(), userID, service.ConfirmRequest{
		OperationID:  operationID,
		DraftVersion: body.DraftVersion,
		Key:          key,
		SimulateMode: body.SimulateMode,
	})
	if err != nil {
		respondError(w, r.log, err)
		return
	}
	w.Header().Set("x-idempotent-replay", boolString(out.Replay))
	writeJSON(r.log, w, nethttp.StatusOK, out)
}

func (r *Router) reconcile(w nethttp.ResponseWriter, req *nethttp.Request) {
	userID, ok := r.user(w, req)
	if !ok {
		return
	}
	operationID, ok := r.operationID(w, req)
	if !ok {
		return
	}
	out, err := r.service.Reconcile(req.Context(), userID, operationID)
	if err != nil {
		respondError(w, r.log, err)
		return
	}
	writeJSON(r.log, w, nethttp.StatusOK, out)
}

func (r *Router) testProfiles(w nethttp.ResponseWriter, _ *nethttp.Request) {
	type item struct {
		UserID   string `json:"user_id"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		TeamID   string `json:"team_id"`
		TeamName string `json:"team_name"`
	}
	profiles := r.seed.Directory.Profiles()
	items := make([]item, 0, len(profiles))
	for _, profile := range profiles {
		teamName, err := r.seed.Directory.TeamName(profile.TeamID)
		if err != nil {
			respondError(w, r.log, err)
			return
		}
		items = append(items, item{
			UserID: string(profile.UserID), Name: profile.Name, Role: string(profile.Role),
			TeamID: string(profile.TeamID), TeamName: teamName,
		})
	}
	writeJSON(r.log, w, nethttp.StatusOK, map[string]any{"profiles": items})
}

// user resolves the synthetic profile. An unknown header value is rejected
// with 401 instead of silently falling back to another identity.
func (r *Router) user(w nethttp.ResponseWriter, req *nethttp.Request) (domain.UserID, bool) {
	raw := strings.TrimSpace(req.Header.Get(testUserHeader))
	if raw == "" {
		raw = defaultTestUser
	}
	id := domain.UserID(raw)
	if _, err := r.seed.Directory.Profile(id); err != nil {
		writeJSON(r.log, w, nethttp.StatusUnauthorized, apiError{
			ErrorCode: "USER_NOT_FOUND",
			Message:   "Выберите доступный тестовый профиль.",
		})
		return "", false
	}
	return id, true
}

func (r *Router) operationID(w nethttp.ResponseWriter, req *nethttp.Request) (domain.OperationID, bool) {
	id := domain.OperationID(strings.TrimSpace(req.PathValue("operation_id")))
	if err := id.Validate(); err != nil {
		respondError(w, r.log, err)
		return "", false
	}
	return id, true
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
