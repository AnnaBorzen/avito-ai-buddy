package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/access"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/ml"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
)

const historyLimit = 20

// Service owns the use cases of the access-request scenario. Policy decisions
// stay in domain; persistence and external calls are injected.
type Service struct {
	store     *store.Store
	seed      domain.Seed
	ml        *ml.Client
	access    *access.Client
	log       *slog.Logger
	eventTick atomic.Uint64
}

func New(st *store.Store, seed domain.Seed, mlClient *ml.Client, accessClient *access.Client, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, seed: seed, ml: mlClient, access: accessClient, log: log}
}

// Context returns the single source of truth for the screen (FR-01, FR-07).
func (s *Service) Context(ctx context.Context, userID domain.UserID) (Context, error) {
	profile, err := s.seed.Directory.Profile(userID)
	if err != nil {
		return Context{}, err
	}
	task, created, err := s.store.EnsureTask(ctx, profile.UserID, s.seed.Scenario.ID)
	if err != nil {
		return Context{}, err
	}
	if created {
		s.record(ctx, domain.AnalyticsEvent{
			Event: domain.EventTaskStarted, ActorRef: profile.UserID,
			TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
			ScenarioVersion: s.seed.Scenario.Version, Outcome: domain.OutcomeOK,
		})
	}
	op, err := s.store.LatestOperation(ctx, profile.UserID)
	if err != nil {
		return Context{}, err
	}
	view, err := s.seed.Directory.UserView(profile.UserID)
	if err != nil {
		return Context{}, err
	}
	scenario, err := domain.ProjectScenario(s.seed.Scenario, domain.ScenarioInput{
		TaskInstanceID: task.ID, RulesExplained: task.RulesExplained, Operation: op,
	})
	if err != nil {
		return Context{}, err
	}
	return Context{
		User: toUser(view), Scenario: toScenario(scenario), ActiveOperation: toOperation(op),
	}, nil
}

// Chat runs one dialog turn. The ML service is stateless; this method assembles
// trusted context, validates the returned tool_call independently and persists
// the task draft and messages.
func (s *Service) Chat(ctx context.Context, userID domain.UserID, req ChatRequest) (ChatResponse, error) {
	profile, err := s.seed.Directory.Profile(userID)
	if err != nil {
		return ChatResponse{}, err
	}
	task, err := s.store.Task(ctx, profile.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ChatResponse{}, fmt.Errorf("%w: %s", ErrTaskNotFound, req.TaskInstanceID)
		}
		return ChatResponse{}, err
	}
	if task.ID != req.TaskInstanceID {
		return ChatResponse{}, fmt.Errorf("%w: %s", ErrTaskNotFound, req.TaskInstanceID)
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return ChatResponse{}, &domain.ValidationError{Field: "message", Message: "must not be empty"}
	}
	if n := len([]rune(message)); n > 2000 {
		return ChatResponse{}, &domain.ValidationError{Field: "message", Message: "must not exceed 2000 characters"}
	}
	mode := "normal"
	if req.SimulateMode != nil {
		mode = *req.SimulateMode
	}
	switch mode {
	case "normal":
	case "service_error":
		return ChatResponse{}, fmt.Errorf("%w: simulated LLM outage", ml.ErrUnavailable)
	default:
		return ChatResponse{}, &domain.ValidationError{Field: "simulate_mode", Message: "must be normal or service_error"}
	}

	op, err := s.store.LatestOperation(ctx, profile.UserID)
	if err != nil {
		return ChatResponse{}, err
	}
	draft := draftFromTask(task)
	history, err := s.history(ctx, task.ID)
	if err != nil {
		return ChatResponse{}, err
	}

	turn, err := s.ml.Turn(ctx, ml.TurnRequest{
		TaskInstanceID: string(task.ID),
		User:           s.mlUser(profile),
		Permissions:    s.permissions(profile),
		Draft:          draft,
		ExpectedSlot:   expectedSlot(draft),
		Operation:      mlOperation(op),
		History:        history,
		Message:        message,
	})
	if err != nil {
		return ChatResponse{}, err
	}
	return s.consumeTurn(ctx, profile, task, op, message, req.HandoffRequested, turn)
}

// ChatRequest is the trusted, already validated chat input.
type ChatRequest struct {
	TaskInstanceID   domain.TaskInstanceID
	Message          string
	HandoffRequested bool
	// SimulateMode is the contract's demo switch: "service_error" makes the
	// turn fail before any state change, as if the ML service were down. The
	// default is applied here, the service owns the accepted values.
	SimulateMode *string
}

func (s *Service) consumeTurn(
	ctx context.Context,
	profile domain.Profile,
	task store.Task,
	op *domain.Operation,
	message string,
	handoffRequested bool,
	turn *ml.TurnResponse,
) (ChatResponse, error) {
	response := ChatResponse{
		TaskInstanceID:   string(task.ID),
		Reply:            turn.Reply,
		Sources:          toSources(turn.Sources),
		HandoffSuggested: turn.HandoffSuggested || handoffRequested,
	}
	if turn.Sources == nil {
		response.Sources = []Source{}
	}

	var denial *RbacDenial
	if turn.ToolCall != nil {
		proposal, err := s.resolveProposal(profile, *turn.ToolCall)
		if denied := new(domain.AccessDeniedError); errors.As(err, &denied) {
			denial = RbacDenialFrom(denied)
			response.Reply = denial.Message
		} else if err != nil {
			return ChatResponse{}, err
		} else {
			op, err = s.propose(ctx, profile, task, op, proposal)
			if err != nil {
				return ChatResponse{}, err
			}
			if op != nil {
				s.record(ctx, domain.AnalyticsEvent{
					Event: domain.EventActionProposed, ActorRef: profile.UserID,
					TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
					ScenarioVersion: s.seed.Scenario.Version,
					OperationID:     &op.ID, Outcome: domain.OutcomeOK,
					ResourceID: &op.Payload.ResourceID, AccessLevel: &op.Payload.AccessLevel,
					DraftVersion: &op.DraftVersion,
				})
			}
		}
	}
	if denial != nil {
		response.RbacDenial = denial
	}
	response.ProposedOperation = toOperation(op)

	slots := turn.Slots
	if op != nil && turn.ToolCall != nil {
		resource, level, justification := string(op.Payload.ResourceID),
			string(op.Payload.AccessLevel), op.Payload.Justification
		slots = ml.Slots{ResourceID: &resource, AccessLevel: &level, Justification: &justification}
	}
	if err := s.store.UpdateTaskSlots(ctx, task.ID,
		mergeString(task.ResourceID, slots.ResourceID),
		mergeString(task.AccessLevel, slots.AccessLevel),
		mergeString(task.Justification, slots.Justification)); err != nil {
		return ChatResponse{}, err
	}

	rulesExplained := turn.ToolCall != nil || turn.Intent == "ask_procedure" || turn.Intent == "ask_levels"
	if rulesExplained && !task.RulesExplained {
		if err := s.store.MarkRulesExplained(ctx, task.ID); err != nil {
			return ChatResponse{}, err
		}
	}
	if err := s.saveMessages(ctx, task.ID, message, turn, handoffRequested); err != nil {
		return ChatResponse{}, err
	}
	if denial == nil {
		s.record(ctx, domain.AnalyticsEvent{
			Event: domain.EventAnswerDelivered, ActorRef: profile.UserID,
			TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
			ScenarioVersion: s.seed.Scenario.Version, Outcome: domain.OutcomeOK,
			ModelVersion: stringPtr(turn.Meta.Model),
			SourceIDs:    sourceIDs(turn.Sources),
		})
	}
	if handoffRequested {
		reason := domain.HandoffUserInitiated
		s.record(ctx, domain.AnalyticsEvent{
			Event: domain.EventHandoffRequested, ActorRef: profile.UserID,
			TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
			ScenarioVersion: s.seed.Scenario.Version, Outcome: domain.OutcomeOK,
			Reason: &reason,
		})
	}
	return response, nil
}
