package service

import (
	"context"
	"fmt"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/ml"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
)

type proposal struct {
	resource domain.Resource
	approver domain.Profile
	input    domain.PayloadInput
}

func (s *Service) resolveProposal(profile domain.Profile, call ml.ToolCall) (proposal, error) {
	if call.Name != "propose_access_request" {
		return proposal{}, &domain.ValidationError{
			Field: "tool_call.name", Message: fmt.Sprintf("unsupported tool %q", call.Name),
		}
	}
	resource, err := s.seed.Catalog.Resource(domain.ResourceID(call.Arguments.ResourceID))
	if err != nil {
		return proposal{}, &domain.ValidationError{
			Field: "tool_call.arguments.resource_id", Message: "resource is not in the catalog",
		}
	}
	level := domain.AccessLevel(call.Arguments.AccessLevel)
	if !level.Valid() {
		return proposal{}, &domain.ValidationError{
			Field: "tool_call.arguments.access_level", Message: "unsupported level",
		}
	}
	if decision := domain.EvaluateAccess(profile, resource, level); !decision.Allowed {
		return proposal{}, &domain.AccessDeniedError{
			UserID: profile.UserID, Role: profile.Role, ResourceID: resource.ID,
			AccessLevel: level, Reason: decision.Reason,
		}
	}
	approver, err := s.seed.Directory.ApproverFor(profile.UserID)
	if err != nil {
		return proposal{}, &domain.ValidationError{
			Field: "approver", Message: "у текущего пользователя нет маршрута согласования",
		}
	}
	input := domain.PayloadInput{
		ResourceID: resource.ID, AccessLevel: level,
		Justification: call.Arguments.Justification,
	}
	// Validate all payload invariants before any state is written.
	if _, err := domain.NewOperationPayload(input, resource, approver); err != nil {
		return proposal{}, err
	}
	return proposal{resource: resource, approver: approver, input: input}, nil
}

// propose creates a draft or revises the current one. The operation owns
// draft_version and the immutable payload snapshot.
func (s *Service) propose(
	ctx context.Context,
	profile domain.Profile,
	task store.Task,
	current *domain.Operation,
	proposal proposal,
) (*domain.Operation, error) {
	now := time.Now().UTC()
	if current != nil {
		switch current.Status {
		case domain.StatusAwaitingConfirmation:
			changed, err := current.Revise(profile, proposal.input, proposal.resource, proposal.approver, now)
			if err != nil {
				return nil, err
			}
			if changed {
				if err := s.store.UpdateOperation(ctx, *current); err != nil {
					return nil, err
				}
			}
			return current, nil
		case domain.StatusSubmitting, domain.StatusSucceeded, domain.StatusUnknown:
			// A confirmed operation is never rewritten by a repeated proposal.
			return current, nil
		}
	}
	id, err := s.store.NextOperationID(ctx)
	if err != nil {
		return nil, err
	}
	op, err := domain.NewAwaitingConfirmation(
		id, profile, task.ID, s.seed.Scenario.ID, proposal.input,
		proposal.resource, proposal.approver, now,
	)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateOperation(ctx, op); err != nil {
		return nil, err
	}
	return &op, nil
}

func (s *Service) permissions(profile domain.Profile) []ml.Permission {
	resources := s.seed.Catalog.Resources()
	out := make([]ml.Permission, 0, len(resources))
	for _, resource := range resources {
		levels := make([]string, 0, len(resource.Levels))
		for _, level := range resource.Levels {
			if level.Requestable() && domain.EvaluateAccess(profile, resource, level).Allowed {
				levels = append(levels, string(level))
			}
		}
		out = append(out, ml.Permission{ResourceID: string(resource.ID), AllowedLevels: levels})
	}
	return out
}

func (s *Service) mlUser(profile domain.Profile) ml.UserContext {
	view, err := s.seed.Directory.UserView(profile.UserID)
	if err != nil {
		return ml.UserContext{
			UserID: string(profile.UserID), Name: profile.Name, Role: string(profile.Role),
			TeamID: string(profile.TeamID), MentorHandle: profile.MentorHandle,
		}
	}
	return ml.UserContext{
		UserID: string(view.UserID), Name: view.Name, Role: string(view.Role),
		TeamID: string(view.TeamID), TeamName: view.TeamName,
		MentorHandle: view.MentorHandle, ApproverName: view.ApproverName,
	}
}

func mlOperation(op *domain.Operation) *ml.Operation {
	if op == nil {
		return nil
	}
	out := &ml.Operation{OperationID: string(op.ID), OperationStatus: string(op.Status)}
	if op.Ticket != nil {
		ticket := &ml.Ticket{
			TicketID: string(op.Ticket.ID), ExternalStatus: string(op.Ticket.ExternalStatus),
		}
		if op.Ticket.ResourceID != nil {
			ticket.ResourceID = string(*op.Ticket.ResourceID)
		}
		if op.Ticket.AccessLevel != nil {
			ticket.AccessLevel = string(*op.Ticket.AccessLevel)
		}
		out.Ticket = ticket
	}
	return out
}

func draftFromTask(task store.Task) *ml.Slots {
	if task.ResourceID == nil && task.AccessLevel == nil && task.Justification == nil {
		return &ml.Slots{}
	}
	return &ml.Slots{
		ResourceID: task.ResourceID, AccessLevel: task.AccessLevel,
		Justification: task.Justification,
	}
}

func expectedSlot(slots *ml.Slots) *string {
	var value string
	switch {
	case slots == nil || slots.ResourceID == nil || *slots.ResourceID == "":
		value = "resource_id"
	case slots.AccessLevel == nil || *slots.AccessLevel == "":
		value = "access_level"
	case slots.Justification == nil || *slots.Justification == "":
		value = "justification"
	default:
		return nil
	}
	return &value
}

func mergeString(current *string, update *string) *string {
	if update == nil {
		return current
	}
	value := *update
	return &value
}

func (s *Service) history(ctx context.Context, taskID domain.TaskInstanceID) ([]ml.Message, error) {
	messages, err := s.store.Messages(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if len(messages) > historyLimit {
		messages = messages[len(messages)-historyLimit:]
	}
	out := make([]ml.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, ml.Message{Role: message.Role, Content: message.Content})
	}
	return out, nil
}

func (s *Service) saveMessages(
	ctx context.Context,
	taskID domain.TaskInstanceID,
	userMessage string,
	turn *ml.TurnResponse,
	handoff bool,
) error {
	if err := s.store.AddMessage(ctx, taskID, store.Message{
		ID: s.newID("msg"), Role: "user", Content: userMessage,
	}); err != nil {
		return err
	}
	assistant := store.Message{
		ID: s.newID("msg"), Role: "assistant", Content: turn.Reply, Handoff: handoff,
	}
	if len(turn.Sources) > 0 {
		source := turn.Sources[0]
		assistant.Source = &store.SourceSnapshot{
			DocID: source.DocID, Title: source.Title, Section: source.Section,
			Excerpt: source.Excerpt, IsSynthetic: source.IsSynthetic,
		}
	}
	return s.store.AddMessage(ctx, taskID, assistant)
}

func (s *Service) newID(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), s.eventTick.Add(1))
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func sourceIDs(sources []ml.Source) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.DocID)
	}
	return out
}
