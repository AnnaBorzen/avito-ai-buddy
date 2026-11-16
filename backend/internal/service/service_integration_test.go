package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/accessservice"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/access"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/ml"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/service"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/testdb"
)

const justification = "Нужен доступ к базе знаний команды для выполнения задач онбординга."

type stack struct {
	service *service.Service
	access  *access.Client
	turns   chan string
}

// newStack wires the real service against a fake ML service and the real
// access-service mock over HTTP, so both client contracts are exercised.
func newStack(t *testing.T, st *store.Store) stack {
	t.Helper()
	turns := make(chan string, 8)
	mlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/turn" {
			http.NotFound(w, r)
			return
		}
		select {
		case body := <-turns:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		case <-time.After(5 * time.Second):
			http.Error(w, "no turn queued", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(mlServer.Close)

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := accessservice.New(addr, st, log)
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("access service stop: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("access service did not stop")
		}
	})
	accessClient := access.New("http://"+addr, 5*time.Second)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if err := accessClient.Ready(ctx); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	seed, err := domain.NewSeed()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := service.New(st, seed, ml.New(mlServer.URL, 5*time.Second), accessClient, log)
	return stack{service: svc, access: accessClient, turns: turns}
}

func turnWithProposal(reply, resource, level, why string) string {
	body, _ := json.Marshal(map[string]any{
		"reply": reply, "intent": "propose",
		"sources": []map[string]any{{
			"doc_id": "doc_01_onboarding_access", "title": "[ТЕСТОВАЯ ИНСТРУКЦИЯ] Доступы",
			"section": "Порядок", "excerpt": "Заявку подтверждает тимлид.",
			"is_synthetic": true, "source_type": "doc", "source_label": "wiki", "score": 0.9,
		}},
		"tool_call": map[string]any{
			"name": "propose_access_request",
			"arguments": map[string]any{
				"resource_id": resource, "access_level": level, "justification": why,
			},
		},
		"meta": map[string]any{"model": "test-model"},
	})
	return string(body)
}

func newTask(t *testing.T, s stack, userID domain.UserID) service.Context {
	t.Helper()
	ctx, err := s.service.Context(context.Background(), userID)
	if err != nil {
		t.Fatalf("context for %s: %v", userID, err)
	}
	if ctx.Scenario.TaskInstanceID == "" {
		t.Fatalf("context for %s has no task_instance_id", userID)
	}
	return ctx
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func TestChatProposalConfirmAndIdempotentReplay(t *testing.T) {
	st := testdb.Spawn(t)
	s := newStack(t, st)
	ctx := context.Background()
	user := domain.UserID("usr_intern_01")
	view := newTask(t, s, user)

	// First turn proposes the draft.
	s.turns <- turnWithProposal("Формирую заявку.", "space_auto_backend_wiki", "read", justification)
	chat, err := s.service.Chat(ctx, user, service.ChatRequest{
		TaskInstanceID: domain.TaskInstanceID(view.Scenario.TaskInstanceID),
		Message:        "Нужен доступ к вики команды",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if chat.ProposedOperation == nil || chat.ProposedOperation.DraftVersion != 1 {
		t.Fatalf("proposed operation = %+v", chat.ProposedOperation)
	}
	operationID := domain.OperationID(chat.ProposedOperation.OperationID)

	// A revised proposal increments the draft version.
	s.turns <- turnWithProposal("Уточняю заявку.", "space_auto_backend_wiki", "read",
		justification+" Нужен именно уровень чтения.")
	chat, err = s.service.Chat(ctx, user, service.ChatRequest{
		TaskInstanceID: domain.TaskInstanceID(view.Scenario.TaskInstanceID),
		Message:        "Только чтение, для онбординга",
	})
	if err != nil {
		t.Fatalf("revise chat: %v", err)
	}
	if chat.ProposedOperation == nil || chat.ProposedOperation.DraftVersion != 2 {
		t.Fatalf("revised operation = %+v", chat.ProposedOperation)
	}

	// A stale confirmation is rejected before any external call.
	stale, err := s.service.Confirm(ctx, user, service.ConfirmRequest{
		OperationID: operationID, DraftVersion: 1, Key: "idem-key-stale-1",
	})
	var conflict *domain.DraftVersionConflictError
	if !errors.As(err, &conflict) || stale.OperationID != "" {
		t.Fatalf("stale confirm = %+v, %v; want draft conflict", stale, err)
	}

	confirmed, err := s.service.Confirm(ctx, user, service.ConfirmRequest{
		OperationID: operationID, DraftVersion: 2, Key: "idem-key-confirm-1",
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.OperationStatus != string(domain.StatusSucceeded) || confirmed.Ticket == nil {
		t.Fatalf("confirmed = %+v", confirmed)
	}
	if confirmed.Replay {
		t.Fatal("first confirmation is not a replay")
	}

	replay, err := s.service.Confirm(ctx, user, service.ConfirmRequest{
		OperationID: operationID, DraftVersion: 2, Key: "idem-key-confirm-1",
	})
	if err != nil {
		t.Fatalf("replay confirm: %v", err)
	}
	if !replay.Replay || replay.Ticket == nil || replay.Ticket.TicketID != confirmed.Ticket.TicketID {
		t.Fatalf("replay = %+v, want stored ticket %+v", replay, confirmed.Ticket)
	}

	// The access service owns exactly one ticket for the operation.
	ticket, found, err := s.access.Lookup(ctx, string(operationID))
	if err != nil || !found || ticket == nil {
		t.Fatalf("access lookup = %+v, %v, %v", ticket, found, err)
	}
	if ticket.ResourceID == nil || *ticket.ResourceID != "space_auto_backend_wiki" {
		t.Fatalf("ticket resource = %+v", ticket)
	}

	active, err := s.service.Context(ctx, user)
	if err != nil {
		t.Fatalf("context after confirm: %v", err)
	}
	if active.ActiveOperation == nil || active.ActiveOperation.OperationStatus != string(domain.StatusSucceeded) {
		t.Fatalf("active operation = %+v", active.ActiveOperation)
	}
}

func TestRbacDenialDoesNotCreateProposal(t *testing.T) {
	st := testdb.Spawn(t)
	s := newStack(t, st)
	user := domain.UserID("usr_intern_01")
	view := newTask(t, s, user)

	s.turns <- turnWithProposal("Проверяю права.", "db_fintech_prod_cluster", "read", justification)
	chat, err := s.service.Chat(context.Background(), user, service.ChatRequest{
		TaskInstanceID: domain.TaskInstanceID(view.Scenario.TaskInstanceID),
		Message:        "Дай доступ к продовой базе",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if chat.RbacDenial == nil || chat.RbacDenial.ErrorCode != "RBAC_ACCESS_DENIED" {
		t.Fatalf("rbac_denial = %+v", chat.RbacDenial)
	}
	if chat.ProposedOperation != nil {
		t.Fatalf("denied turn created operation %+v", chat.ProposedOperation)
	}
	after, err := s.service.Context(context.Background(), user)
	if err != nil {
		t.Fatalf("context after denial: %v", err)
	}
	if after.ActiveOperation != nil {
		t.Fatalf("active operation after denial = %+v", after.ActiveOperation)
	}
}

func TestTimeoutThenReconcileResolvesLostResponse(t *testing.T) {
	st := testdb.Spawn(t)
	s := newStack(t, st)
	ctx := context.Background()
	user := domain.UserID("usr_intern_02")
	view := newTask(t, s, user)

	s.turns <- turnWithProposal("Формирую заявку.", "space_goods_search_wiki", "read", justification)
	chat, err := s.service.Chat(ctx, user, service.ChatRequest{
		TaskInstanceID: domain.TaskInstanceID(view.Scenario.TaskInstanceID),
		Message:        "Нужен доступ к вики поиска",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if chat.ProposedOperation == nil {
		t.Fatal("chat did not propose an operation")
	}
	proposal := chat.ProposedOperation
	mode := "timeout"
	_, err = s.service.Confirm(ctx, user, service.ConfirmRequest{
		OperationID: domain.OperationID(proposal.OperationID), DraftVersion: proposal.DraftVersion,
		Key: "idem-key-timeout-1", SimulateMode: &mode,
	})
	var upstream *service.UpstreamError
	if !errors.As(err, &upstream) || !errors.Is(upstream, service.ErrUpstreamTimeout) {
		t.Fatalf("timeout confirm = %v, want upstream timeout", err)
	}
	if upstream.Status != domain.StatusUnknown {
		t.Fatalf("operation status after timeout = %s, want unknown", upstream.Status)
	}

	// The remote service actually created the ticket before the response was
	// lost; reconcile must find it instead of resubmitting.
	userView := view.User
	if _, err := s.access.Create(ctx, "idem-key-recover-1", access.CreateRequest{
		OperationID: proposal.OperationID, UserID: userView.UserID, TeamID: userView.TeamID,
		ResourceID: proposal.Payload.ResourceID, AccessLevel: proposal.Payload.AccessLevel,
		Justification: proposal.Payload.Justification, ApproverID: proposal.Payload.ApproverID,
		ApproverName: proposal.Payload.ApproverName,
	}); err != nil {
		t.Fatalf("remote ticket: %v", err)
	}
	reconciled, err := s.service.Reconcile(ctx, user, domain.OperationID(proposal.OperationID))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reconciled.OperationStatus != string(domain.StatusSucceeded) || reconciled.Ticket == nil {
		t.Fatalf("reconciled = %+v", reconciled)
	}
}

func TestReconcileWithoutRemoteTicketFailsOperation(t *testing.T) {
	st := testdb.Spawn(t)
	s := newStack(t, st)
	ctx := context.Background()
	user := domain.UserID("usr_intern_02")
	view := newTask(t, s, user)

	s.turns <- turnWithProposal("Формирую заявку.", "space_goods_search_wiki", "read", justification)
	chat, err := s.service.Chat(ctx, user, service.ChatRequest{
		TaskInstanceID: domain.TaskInstanceID(view.Scenario.TaskInstanceID),
		Message:        "Нужен доступ к вики поиска",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	proposal := chat.ProposedOperation
	if proposal == nil {
		t.Fatal("chat did not propose an operation")
	}
	mode := "timeout"
	if _, err := s.service.Confirm(ctx, user, service.ConfirmRequest{
		OperationID: domain.OperationID(proposal.OperationID), DraftVersion: proposal.DraftVersion,
		Key: "idem-key-timeout-2", SimulateMode: &mode,
	}); err == nil {
		t.Fatal("timeout confirm succeeded")
	}
	reconciled, err := s.service.Reconcile(ctx, user, domain.OperationID(proposal.OperationID))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reconciled.OperationStatus != string(domain.StatusFailed) || reconciled.Ticket != nil {
		t.Fatalf("reconciled = %+v, want failed without ticket", reconciled)
	}
}

func TestChatSimulateServiceErrorKeepsState(t *testing.T) {
	st := testdb.Spawn(t)
	s := newStack(t, st)
	ctx := context.Background()
	user := domain.UserID("usr_intern_01")
	view := newTask(t, s, user)
	// Do not queue a turn: service_error must fail before calling ML.
	mode := "service_error"
	_, err := s.service.Chat(ctx, user, service.ChatRequest{
		TaskInstanceID: domain.TaskInstanceID(view.Scenario.TaskInstanceID),
		Message:        "Привет", SimulateMode: &mode,
	})
	if !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("chat simulate service_error = %v, want ml unavailable", err)
	}
	after, err := s.service.Context(ctx, user)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if after.ActiveOperation != nil || after.Scenario.StepStatus != "in_progress" {
		t.Fatalf("state changed after failed turn: %+v", after)
	}
}
