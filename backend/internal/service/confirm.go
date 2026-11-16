package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/access"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
)

// ConfirmRequest is the trusted confirmation input from the transport.
type ConfirmRequest struct {
	OperationID  domain.OperationID
	DraftVersion int
	Key          domain.IdempotencyKey
	// SimulateMode is the contract's demo switch for the loss-of-response and
	// upstream-failure scenarios. The state transition matches the real path.
	// The default is applied here, the service owns the accepted values.
	SimulateMode *string
}

// Confirm sends the card exactly once. The operation_id and idempotency key
// scope every retry, so a repeated confirmation replays the stored response.
func (s *Service) Confirm(ctx context.Context, userID domain.UserID, req ConfirmRequest) (ConfirmResponse, error) {
	started := time.Now()
	if err := req.Key.Validate(); err != nil {
		return ConfirmResponse{}, err
	}
	if req.DraftVersion < 1 {
		return ConfirmResponse{}, &domain.ValidationError{Field: "draft_version", Message: "must be at least 1"}
	}
	mode := "normal"
	if req.SimulateMode != nil {
		mode = *req.SimulateMode
	}
	switch mode {
	case "normal", "timeout", "service_error":
	default:
		return ConfirmResponse{}, &domain.ValidationError{
			Field: "simulate_mode", Message: "must be normal, timeout or service_error",
		}
	}
	profile, err := s.seed.Directory.Profile(userID)
	if err != nil {
		return ConfirmResponse{}, err
	}
	task, err := s.store.Task(ctx, profile.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ConfirmResponse{}, fmt.Errorf("%w: operation %s", ErrTaskNotFound, req.OperationID)
		}
		return ConfirmResponse{}, err
	}
	op, err := s.store.Operation(ctx, profile.UserID, req.OperationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ConfirmResponse{}, fmt.Errorf("%w: operation %s", ErrTaskNotFound, req.OperationID)
		}
		return ConfirmResponse{}, err
	}
	if op.TaskInstanceID != task.ID {
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s", ErrTaskNotFound, req.OperationID)
	}

	hash := requestHash(req)
	existing, err := s.store.Idempotency(ctx, profile.UserID, op.ID, req.Key)
	if err != nil {
		return ConfirmResponse{}, err
	}
	switch domain.DecideIdempotency(existing, hash) {
	case domain.IdempotencyConflict:
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s", ErrIdempotencyKeyReused, op.ID)
	case domain.IdempotencyInProgress:
		// A previous attempt may have committed the operation but failed to
		// persist the response (server restart, DB error). The stored operation
		// is authoritative, so replay it instead of blocking the key forever.
		if op.Status == domain.StatusSucceeded {
			replay, err := s.confirmResponse(task, &op)
			if err != nil {
				return ConfirmResponse{}, err
			}
			replay.Replay = true
			return replay, nil
		}
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s", ErrIdempotencyInProgress, op.ID)
	case domain.IdempotencyReplay:
		var replay ConfirmResponse
		if err := json.Unmarshal(existing.ResponseBody, &replay); err != nil {
			return ConfirmResponse{}, fmt.Errorf("service: decode idempotent response: %w", err)
		}
		replay.Replay = true
		return replay, nil
	}

	switch op.Status {
	case domain.StatusSucceeded:
		return s.confirmResponse(task, &op)
	case domain.StatusAwaitingConfirmation:
	case domain.StatusSubmitting:
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s is submitting", ErrOperationAlreadyFinished, op.ID)
	case domain.StatusUnknown:
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s must be reconciled", ErrOperationAlreadyFinished, op.ID)
	case domain.StatusFailed:
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s failed; create a new request", ErrOperationAlreadyFinished, op.ID)
	default:
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s is %s", ErrOperationAlreadyFinished, op.ID, op.Status)
	}

	if err := op.CheckConfirmation(req.DraftVersion); err != nil {
		return ConfirmResponse{}, err
	}
	if err := s.recheckPolicy(profile, op); err != nil {
		return ConfirmResponse{}, err
	}

	record := domain.IdempotencyRecord{
		UserID: profile.UserID, OperationID: op.ID, Key: req.Key,
		RequestHash: hash, CreatedAt: time.Now().UTC(),
	}
	reserved, err := s.store.ReserveIdempotency(ctx, record)
	if err != nil {
		return ConfirmResponse{}, err
	}
	if !reserved {
		return ConfirmResponse{}, fmt.Errorf("%w: operation %s", ErrIdempotencyInProgress, op.ID)
	}

	now := time.Now().UTC()
	if err := op.BeginSubmission(now); err != nil {
		s.releaseKey(ctx, profile.UserID, op.ID, req.Key)
		return ConfirmResponse{}, err
	}
	if err := s.store.UpdateOperation(ctx, op); err != nil {
		s.releaseKey(ctx, profile.UserID, op.ID, req.Key)
		return ConfirmResponse{}, err
	}
	draftVersion := op.DraftVersion
	s.record(ctx, domain.AnalyticsEvent{
		Event: domain.EventActionConfirmed, ActorRef: profile.UserID,
		TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
		ScenarioVersion: s.seed.Scenario.Version, OperationID: &op.ID,
		Outcome: domain.OutcomeOK, DraftVersion: &draftVersion,
	})

	switch mode {
	case "timeout":
		s.releaseKey(ctx, profile.UserID, op.ID, req.Key)
		return s.markUnknown(ctx, &op, task, int(time.Since(started).Milliseconds()),
			"UPSTREAM_TIMEOUT", fmt.Errorf("%w: simulated lost response", access.ErrTimeout))
	case "service_error":
		return s.failSubmission(ctx, &op, task, req.Key, int(time.Since(started).Milliseconds()),
			fmt.Errorf("%w: simulated service failure", access.ErrUnavailable))
	}

	ticket, err := s.access.Create(ctx, string(req.Key), access.CreateRequest{
		OperationID: string(op.ID), UserID: string(profile.UserID),
		TeamID: string(profile.TeamID), ResourceID: string(op.Payload.ResourceID),
		AccessLevel: string(op.Payload.AccessLevel), Justification: op.Payload.Justification,
		ApproverID: string(op.Payload.ApproverID), ApproverName: op.Payload.ApproverName,
	})
	duration := int(time.Since(started).Milliseconds())
	if err != nil {
		return s.failSubmission(ctx, &op, task, req.Key, duration, err)
	}
	return s.finishSubmission(ctx, profile, &op, task, ticket, req.Key, duration)
}

func (s *Service) finishSubmission(
	ctx context.Context,
	profile domain.Profile,
	op *domain.Operation,
	task store.Task,
	remote *access.Ticket,
	key domain.IdempotencyKey,
	durationMS int,
) (ConfirmResponse, error) {
	ticket, err := toDomainTicket(op.ID, remote)
	if err != nil {
		s.log.ErrorContext(ctx, "invalid access-service ticket; operation marked unknown",
			"operation_id", op.ID, "error", err)
		return s.markUnknown(ctx, op, task, durationMS, "INVALID_UPSTREAM_RESPONSE", err)
	}
	if err := op.MarkSucceeded(ticket, time.Now().UTC()); err != nil {
		return s.markUnknown(ctx, op, task, durationMS, "INVALID_UPSTREAM_RESPONSE", err)
	}
	if err := s.store.UpdateOperation(ctx, *op); err != nil {
		return ConfirmResponse{}, err
	}
	response, err := s.confirmResponse(task, op)
	if err != nil {
		return ConfirmResponse{}, err
	}
	s.completeKey(ctx, profile.UserID, op.ID, key, response)
	duration := durationMS
	ticketID := ticket.ID
	recovered := false
	s.record(ctx, domain.AnalyticsEvent{
		Event: domain.EventActionSucceeded, ActorRef: profile.UserID,
		TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
		ScenarioVersion: s.seed.Scenario.Version, OperationID: &op.ID,
		Outcome: domain.OutcomeOK, DurationMS: &duration,
		TicketID: &ticketID, Recovered: &recovered,
	})
	return response, nil
}

func (s *Service) failSubmission(
	ctx context.Context,
	op *domain.Operation,
	task store.Task,
	key domain.IdempotencyKey,
	durationMS int,
	cause error,
) (ConfirmResponse, error) {
	s.releaseKey(ctx, op.UserID, op.ID, key)
	if errors.Is(cause, access.ErrTimeout) {
		return s.markUnknown(ctx, op, task, durationMS, "UPSTREAM_TIMEOUT", cause)
	}
	code := "UPSTREAM_ERROR"
	kind := ErrUpstreamError
	if errors.Is(cause, access.ErrRejected) {
		code = "UPSTREAM_REJECTED"
	}
	if err := op.MarkFailed(time.Now().UTC()); err != nil {
		return ConfirmResponse{}, err
	}
	if err := s.store.UpdateOperation(ctx, *op); err != nil {
		return ConfirmResponse{}, err
	}
	duration := durationMS
	recovered := false
	s.record(ctx, domain.AnalyticsEvent{
		Event: domain.EventActionFailed, ActorRef: op.UserID,
		TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
		ScenarioVersion: s.seed.Scenario.Version, OperationID: &op.ID,
		Outcome: domain.OutcomeError, ErrorCode: &code,
		DurationMS: &duration, Recovered: &recovered,
	})
	return ConfirmResponse{}, &UpstreamError{
		Kind: kind, Operation: op.ID, Status: op.Status, Cause: cause,
	}
}

func (s *Service) markUnknown(
	ctx context.Context,
	op *domain.Operation,
	task store.Task,
	durationMS int,
	code string,
	cause error,
) (ConfirmResponse, error) {
	if err := op.MarkUnknown(time.Now().UTC()); err != nil {
		return ConfirmResponse{}, err
	}
	if err := s.store.UpdateOperation(ctx, *op); err != nil {
		return ConfirmResponse{}, err
	}
	duration := durationMS
	s.record(ctx, domain.AnalyticsEvent{
		Event: domain.EventActionStatusUnknown, ActorRef: op.UserID,
		TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
		ScenarioVersion: s.seed.Scenario.Version, OperationID: &op.ID,
		Outcome: domain.OutcomeUnknown, ErrorCode: &code, DurationMS: &duration,
	})
	kind := ErrUpstreamError
	if code == "UPSTREAM_TIMEOUT" {
		kind = ErrUpstreamTimeout
	}
	return ConfirmResponse{}, &UpstreamError{
		Kind: kind, Operation: op.ID, Status: op.Status, Cause: cause,
	}
}

func (s *Service) confirmResponse(task store.Task, op *domain.Operation) (ConfirmResponse, error) {
	scenario, err := domain.ProjectScenario(s.seed.Scenario, domain.ScenarioInput{
		TaskInstanceID: task.ID, RulesExplained: task.RulesExplained, Operation: op,
	})
	if err != nil {
		return ConfirmResponse{}, err
	}
	return ConfirmResponse{
		OperationID: string(op.ID), OperationStatus: string(op.Status),
		Ticket: toTicket(op.Ticket), ScenarioStepStatus: string(scenario.StepStatus),
		NextStepHint: scenario.NextStepHint,
	}, nil
}

func (s *Service) recheckPolicy(profile domain.Profile, op domain.Operation) error {
	resource, err := s.seed.Catalog.Resource(op.Payload.ResourceID)
	if err != nil {
		return &domain.ValidationError{Field: "resource_id", Message: "resource is no longer available"}
	}
	if decision := domain.EvaluateAccess(profile, resource, op.Payload.AccessLevel); !decision.Allowed {
		return &domain.AccessDeniedError{
			UserID: profile.UserID, Role: profile.Role, ResourceID: resource.ID,
			AccessLevel: op.Payload.AccessLevel, Reason: decision.Reason,
		}
	}
	approver, err := s.seed.Directory.ApproverFor(profile.UserID)
	if err != nil {
		return &domain.ValidationError{Field: "approver", Message: "маршрут согласования больше не доступен"}
	}
	_, err = domain.NewOperationPayload(domain.PayloadInput{
		ResourceID: op.Payload.ResourceID, AccessLevel: op.Payload.AccessLevel,
		Justification: op.Payload.Justification,
	}, resource, approver)
	return err
}

func toDomainTicket(operationID domain.OperationID, remote *access.Ticket) (domain.Ticket, error) {
	if remote == nil {
		return domain.Ticket{}, &domain.ValidationError{Field: "ticket", Message: "access service returned no ticket"}
	}
	var resource *domain.ResourceID
	if remote.ResourceID != nil {
		value := domain.ResourceID(*remote.ResourceID)
		resource = &value
	}
	var level *domain.AccessLevel
	if remote.AccessLevel != nil {
		value := domain.AccessLevel(*remote.AccessLevel)
		level = &value
	}
	return domain.NewTicket(
		domain.TicketID(remote.TicketID), operationID,
		domain.TicketStatus(remote.ExternalStatus), resource, level,
		remote.ApproverName, remote.CreatedAt,
	)
}

func requestHash(req ConfirmRequest) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", req.OperationID, req.DraftVersion)))
	return hex.EncodeToString(sum[:])
}

func (s *Service) completeKey(
	ctx context.Context,
	userID domain.UserID,
	operationID domain.OperationID,
	key domain.IdempotencyKey,
	response ConfirmResponse,
) {
	body, err := json.Marshal(response)
	if err != nil {
		s.log.ErrorContext(ctx, "encode idempotent response", "operation_id", operationID, "error", err)
		return
	}
	if err := s.store.CompleteIdempotency(ctx, userID, operationID, key, 200, body); err != nil {
		s.log.ErrorContext(ctx, "complete idempotency key", "operation_id", operationID, "error", err)
	}
}

func (s *Service) releaseKey(ctx context.Context, userID domain.UserID, operationID domain.OperationID, key domain.IdempotencyKey) {
	if key == "" {
		return
	}
	if err := s.store.DeleteIdempotency(ctx, userID, operationID, key); err != nil {
		s.log.ErrorContext(ctx, "release idempotency key", "operation_id", operationID, "error", err)
	}
}
