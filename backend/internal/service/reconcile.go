package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
)

// Reconcile resolves an operation whose create response was lost. It only
// looks up the ticket and never resubmits the request.
func (s *Service) Reconcile(ctx context.Context, userID domain.UserID, operationID domain.OperationID) (ReconcileResponse, error) {
	profile, err := s.seed.Directory.Profile(userID)
	if err != nil {
		return ReconcileResponse{}, err
	}
	op, err := s.store.Operation(ctx, profile.UserID, operationID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ReconcileResponse{}, fmt.Errorf("%w: operation %s", ErrTaskNotFound, operationID)
		}
		return ReconcileResponse{}, err
	}
	switch op.Status {
	case domain.StatusSucceeded:
		return ReconcileResponse{
			OperationID: string(op.ID), OperationStatus: string(op.Status),
			Ticket: toTicket(op.Ticket), Message: "Заявка уже создана.",
		}, nil
	case domain.StatusFailed:
		return ReconcileResponse{
			OperationID: string(op.ID), OperationStatus: string(op.Status),
			Message: "Сервис подтвердил, что заявка не создана. Можно сформировать новую.",
		}, nil
	case domain.StatusUnknown:
	default:
		return ReconcileResponse{}, fmt.Errorf("%w: operation %s is %s",
			ErrOperationAlreadyFinished, op.ID, op.Status)
	}

	remote, found, err := s.access.Lookup(ctx, string(op.ID))
	if err != nil {
		return ReconcileResponse{}, err
	}
	task, err := s.store.Task(ctx, profile.UserID)
	if err != nil {
		return ReconcileResponse{}, err
	}
	if !found {
		return s.reconcileMissing(ctx, profile, &op, task)
	}
	ticket, err := toDomainTicket(op.ID, remote)
	if err != nil {
		return ReconcileResponse{}, err
	}
	if err := op.ReconcileFound(ticket, time.Now().UTC()); err != nil {
		return ReconcileResponse{}, err
	}
	if err := s.store.UpdateOperation(ctx, op); err != nil {
		return ReconcileResponse{}, err
	}
	zero, recovered := 0, true
	ticketID := ticket.ID
	s.record(ctx, domain.AnalyticsEvent{
		Event: domain.EventActionSucceeded, ActorRef: profile.UserID,
		TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
		ScenarioVersion: s.seed.Scenario.Version, OperationID: &op.ID,
		Outcome: domain.OutcomeOK, DurationMS: &zero,
		TicketID: &ticketID, Recovered: &recovered,
	})
	return ReconcileResponse{
		OperationID: string(op.ID), OperationStatus: string(op.Status),
		Ticket: toTicket(op.Ticket), Message: "Заявка найдена в сервисе доступа.",
	}, nil
}

func (s *Service) reconcileMissing(
	ctx context.Context,
	profile domain.Profile,
	op *domain.Operation,
	task store.Task,
) (ReconcileResponse, error) {
	if err := op.ReconcileNotFound(time.Now().UTC()); err != nil {
		return ReconcileResponse{}, err
	}
	if err := s.store.UpdateOperation(ctx, *op); err != nil {
		return ReconcileResponse{}, err
	}
	code := "NOT_FOUND_ON_RECONCILE"
	zero, recovered := 0, true
	s.record(ctx, domain.AnalyticsEvent{
		Event: domain.EventActionFailed, ActorRef: profile.UserID,
		TaskInstanceID: task.ID, ScenarioID: s.seed.Scenario.ID,
		ScenarioVersion: s.seed.Scenario.Version, OperationID: &op.ID,
		Outcome: domain.OutcomeError, ErrorCode: &code,
		DurationMS: &zero, Recovered: &recovered,
	})
	return ReconcileResponse{
		OperationID: string(op.ID), OperationStatus: string(op.Status),
		Message: "Сервис доступа не нашёл заявку. Можно сформировать новую.",
	}, nil
}
