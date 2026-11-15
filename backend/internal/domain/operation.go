package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	minJustification = 10
	maxJustification = 300
)

// OperationStatus is the state of one confirmed action.
type OperationStatus string

const (
	StatusDraft                OperationStatus = "draft"
	StatusAwaitingConfirmation OperationStatus = "awaiting_confirmation"
	StatusSubmitting           OperationStatus = "submitting"
	StatusSucceeded            OperationStatus = "succeeded"
	StatusFailed               OperationStatus = "failed"
	StatusUnknown              OperationStatus = "unknown"
)

func (s OperationStatus) Valid() bool {
	switch s {
	case StatusDraft,
		StatusAwaitingConfirmation,
		StatusSubmitting,
		StatusSucceeded,
		StatusFailed,
		StatusUnknown:
		return true
	default:
		return false
	}
}

// Terminal reports whether the operation itself cannot transition anymore.
// A failed operation is terminal; retrying means creating a new operation.
func (s OperationStatus) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed
}

// PayloadInput is the only part of a proposal that comes from the user or LLM.
// User, team and approver fields are deliberately absent.
type PayloadInput struct {
	ResourceID    ResourceID
	AccessLevel   AccessLevel
	Justification string
}

// OperationPayload is an immutable snapshot of the card shown to the user.
type OperationPayload struct {
	ResourceID    ResourceID
	ResourceTitle string
	AccessLevel   AccessLevel
	Justification string
	ApproverID    UserID
	ApproverName  string
}

func NewOperationPayload(input PayloadInput, resource Resource, approver Profile) (OperationPayload, error) {
	if err := input.ResourceID.Validate(); err != nil {
		return OperationPayload{}, err
	}
	if input.ResourceID != resource.ID {
		return OperationPayload{}, &InvariantError{Message: "input resource and resolved resource differ"}
	}
	if !input.AccessLevel.Requestable() {
		return OperationPayload{}, &ValidationError{
			Field:   "payload.access_level",
			Message: fmt.Sprintf("level %q is not requestable", input.AccessLevel),
		}
	}
	if !resource.Allows(input.AccessLevel) {
		return OperationPayload{}, &ValidationError{
			Field:   "payload.access_level",
			Message: fmt.Sprintf("resource %q does not support level %q", resource.ID, input.AccessLevel),
		}
	}
	if err := approver.Validate(); err != nil {
		return OperationPayload{}, err
	}

	justification := strings.TrimSpace(input.Justification)
	payload := OperationPayload{
		ResourceID:    resource.ID,
		ResourceTitle: resource.Title,
		AccessLevel:   input.AccessLevel,
		Justification: justification,
		ApproverID:    approver.UserID,
		ApproverName:  approver.Name,
	}
	if err := payload.Validate(); err != nil {
		return OperationPayload{}, err
	}
	return payload, nil
}

func (p OperationPayload) Validate() error {
	if err := p.ResourceID.Validate(); err != nil {
		return err
	}
	if err := requireNonEmpty("payload.resource_title", p.ResourceTitle); err != nil {
		return err
	}
	if !p.AccessLevel.Requestable() {
		return &ValidationError{
			Field:   "payload.access_level",
			Message: fmt.Sprintf("level %q is not requestable", p.AccessLevel),
		}
	}
	n := utf8.RuneCountInString(p.Justification)
	if n < minJustification || n > maxJustification {
		return &ValidationError{
			Field:   "payload.justification",
			Message: fmt.Sprintf("length must be between %d and %d characters, got %d", minJustification, maxJustification, n),
		}
	}
	if err := p.ApproverID.Validate(); err != nil {
		return err
	}
	return requireNonEmpty("payload.approver_name", p.ApproverName)
}

// Operation is one access-request action. Payload and ticket are snapshots; a
// succeeded operation is never overwritten.
type Operation struct {
	ID             OperationID
	UserID         UserID
	TaskInstanceID TaskInstanceID
	ScenarioID     ScenarioID
	Status         OperationStatus
	DraftVersion   int
	Payload        OperationPayload
	Ticket         *Ticket
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewAwaitingConfirmation creates the state produced by propose_access_request.
// The policy is evaluated here, so every caller gets the same RBAC decision.
func NewAwaitingConfirmation(
	id OperationID,
	user Profile,
	taskInstanceID TaskInstanceID,
	scenarioID ScenarioID,
	input PayloadInput,
	resource Resource,
	approver Profile,
	at time.Time,
) (Operation, error) {
	if err := id.Validate(); err != nil {
		return Operation{}, err
	}
	if err := user.Validate(); err != nil {
		return Operation{}, err
	}
	if err := validateApprover(user, approver); err != nil {
		return Operation{}, err
	}
	if err := taskInstanceID.Validate(); err != nil {
		return Operation{}, err
	}
	if err := scenarioID.Validate(); err != nil {
		return Operation{}, err
	}
	if at.IsZero() {
		return Operation{}, &ValidationError{Field: "operation.created_at", Message: "must be set"}
	}
	if err := resource.Validate(); err != nil {
		return Operation{}, err
	}

	decision := EvaluateAccess(user, resource, input.AccessLevel)
	if !decision.Allowed {
		return Operation{}, &AccessDeniedError{
			UserID:      user.UserID,
			Role:        user.Role,
			ResourceID:  resource.ID,
			AccessLevel: input.AccessLevel,
			Reason:      decision.Reason,
		}
	}

	payload, err := NewOperationPayload(input, resource, approver)
	if err != nil {
		return Operation{}, err
	}

	op := Operation{
		ID:             id,
		UserID:         user.UserID,
		TaskInstanceID: taskInstanceID,
		ScenarioID:     scenarioID,
		Status:         StatusAwaitingConfirmation,
		DraftVersion:   1,
		Payload:        payload,
		CreatedAt:      at.UTC(),
		UpdatedAt:      at.UTC(),
	}
	if err := op.Validate(); err != nil {
		return Operation{}, err
	}
	return op, nil
}

func validateApprover(user, approver Profile) error {
	if approver.Role != RoleLead {
		return &InvariantError{Message: "access approver must have the lead role"}
	}
	if approver.TeamID != user.TeamID {
		return &InvariantError{Message: "access approver must lead the user's team"}
	}
	return nil
}

func (o Operation) Validate() error {
	if err := o.ID.Validate(); err != nil {
		return err
	}
	if err := o.UserID.Validate(); err != nil {
		return err
	}
	if err := o.TaskInstanceID.Validate(); err != nil {
		return err
	}
	if err := o.ScenarioID.Validate(); err != nil {
		return err
	}
	if !o.Status.Valid() {
		return &ValidationError{
			Field:   "operation.status",
			Message: fmt.Sprintf("unsupported status %q", o.Status),
		}
	}
	if o.DraftVersion < 1 {
		return &ValidationError{Field: "operation.draft_version", Message: "must be at least 1"}
	}
	if o.CreatedAt.IsZero() {
		return &ValidationError{Field: "operation.created_at", Message: "must be set"}
	}
	if o.UpdatedAt.IsZero() {
		return &ValidationError{Field: "operation.updated_at", Message: "must be set"}
	}
	if o.UpdatedAt.Before(o.CreatedAt) {
		return &ValidationError{Field: "operation.updated_at", Message: "must not precede created_at"}
	}

	if o.Status != StatusDraft {
		if err := o.Payload.Validate(); err != nil {
			return err
		}
	}

	switch o.Status {
	case StatusSucceeded:
		if o.Ticket == nil {
			return &InvariantError{Message: "succeeded operation must have a ticket"}
		}
		if err := o.Ticket.Validate(); err != nil {
			return err
		}
		if o.Ticket.OperationID != o.ID {
			return &InvariantError{Message: "ticket belongs to another operation"}
		}
	default:
		if o.Ticket != nil {
			return &InvariantError{Message: "only a succeeded operation may have a ticket"}
		}
	}
	return nil
}

// Revise updates the payload of an awaiting operation. If nothing significant
// changed, the draft version is not incremented and false is returned.
func (o *Operation) Revise(
	user Profile,
	input PayloadInput,
	resource Resource,
	approver Profile,
	at time.Time,
) (bool, error) {
	if user.UserID != o.UserID {
		return false, &InvariantError{Message: "profile does not own the operation"}
	}
	if err := validateApprover(user, approver); err != nil {
		return false, err
	}
	if o.Status != StatusAwaitingConfirmation {
		return false, &ConflictError{Message: fmt.Sprintf("cannot revise operation in status %q", o.Status)}
	}
	if at.IsZero() {
		return false, &ValidationError{Field: "operation.updated_at", Message: "must be set"}
	}
	if err := resource.Validate(); err != nil {
		return false, err
	}

	decision := EvaluateAccess(user, resource, input.AccessLevel)
	if !decision.Allowed {
		return false, &AccessDeniedError{
			UserID:      user.UserID,
			Role:        user.Role,
			ResourceID:  resource.ID,
			AccessLevel: input.AccessLevel,
			Reason:      decision.Reason,
		}
	}

	payload, err := NewOperationPayload(input, resource, approver)
	if err != nil {
		return false, err
	}
	if payload == o.Payload {
		return false, nil
	}

	next := *o
	next.Payload = payload
	next.DraftVersion++
	next.UpdatedAt = at.UTC()
	if err := next.Validate(); err != nil {
		return false, err
	}
	*o = next
	return true, nil
}

// CheckConfirmation validates the draft version without changing state. The
// service performs RBAC checks through EvaluateAccess before calling it.
func (o Operation) CheckConfirmation(draftVersion int) error {
	if o.Status != StatusAwaitingConfirmation {
		return &ConflictError{Message: fmt.Sprintf("cannot confirm operation in status %q", o.Status)}
	}
	if draftVersion != o.DraftVersion {
		return &DraftVersionConflictError{Expected: o.DraftVersion, Actual: draftVersion}
	}
	return nil
}

func (o *Operation) BeginSubmission(at time.Time) error {
	if o.Status != StatusAwaitingConfirmation {
		return &ConflictError{Message: fmt.Sprintf("cannot submit operation in status %q", o.Status)}
	}
	next, err := o.withUpdatedAt(at)
	if err != nil {
		return err
	}
	next.Status = StatusSubmitting
	if err := next.Validate(); err != nil {
		return err
	}
	*o = next
	return nil
}

// MarkSucceeded records the exactly-once success and its ticket snapshot.
func (o *Operation) MarkSucceeded(ticket Ticket, at time.Time) error {
	if o.Status != StatusSubmitting {
		return &ConflictError{Message: fmt.Sprintf("cannot succeed operation in status %q", o.Status)}
	}
	if o.Ticket != nil {
		return &InvariantError{Message: "operation already has a ticket"}
	}
	if err := ticket.Validate(); err != nil {
		return err
	}
	if ticket.OperationID != o.ID {
		return &InvariantError{Message: "ticket belongs to another operation"}
	}

	next, err := o.withUpdatedAt(at)
	if err != nil {
		return err
	}
	next.Status = StatusSucceeded
	next.Ticket = &ticket
	if err := next.Validate(); err != nil {
		return err
	}
	*o = next
	return nil
}

// MarkFailed records an explicit failure reported by the external service.
func (o *Operation) MarkFailed(at time.Time) error {
	if o.Status != StatusSubmitting {
		return &ConflictError{Message: fmt.Sprintf("cannot fail operation in status %q", o.Status)}
	}
	return o.transitionStatus(StatusFailed, at)
}

// MarkUnknown records an ambiguous external result. A subsequent reconcile
// resolves it to succeeded or failed; it never resubmits the action.
func (o *Operation) MarkUnknown(at time.Time) error {
	if o.Status != StatusSubmitting {
		return &ConflictError{Message: fmt.Sprintf("cannot mark operation unknown in status %q", o.Status)}
	}
	return o.transitionStatus(StatusUnknown, at)
}

// ReconcileFound resolves an unknown operation using the external ticket.
func (o *Operation) ReconcileFound(ticket Ticket, at time.Time) error {
	if o.Status != StatusUnknown {
		return &ConflictError{Message: fmt.Sprintf("cannot reconcile operation in status %q", o.Status)}
	}
	if o.Ticket != nil {
		return &InvariantError{Message: "unknown operation must not have a ticket"}
	}
	if err := ticket.Validate(); err != nil {
		return err
	}
	if ticket.OperationID != o.ID {
		return &InvariantError{Message: "ticket belongs to another operation"}
	}

	next, err := o.withUpdatedAt(at)
	if err != nil {
		return err
	}
	next.Status = StatusSucceeded
	next.Ticket = &ticket
	if err := next.Validate(); err != nil {
		return err
	}
	*o = next
	return nil
}

// ReconcileNotFound resolves an unknown operation as not created externally.
func (o *Operation) ReconcileNotFound(at time.Time) error {
	if o.Status != StatusUnknown {
		return &ConflictError{Message: fmt.Sprintf("cannot reconcile operation in status %q", o.Status)}
	}
	return o.transitionStatus(StatusFailed, at)
}

func (o Operation) withUpdatedAt(at time.Time) (Operation, error) {
	if at.IsZero() {
		return Operation{}, &ValidationError{Field: "operation.updated_at", Message: "must be set"}
	}
	o.UpdatedAt = at.UTC()
	return o, nil
}

func (o *Operation) transitionStatus(status OperationStatus, at time.Time) error {
	next, err := o.withUpdatedAt(at)
	if err != nil {
		return err
	}
	next.Status = status
	if err := next.Validate(); err != nil {
		return err
	}
	*o = next
	return nil
}
