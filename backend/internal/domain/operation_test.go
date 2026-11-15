package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewAwaitingConfirmation(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	input := defaultPayload(fixture.Resource)
	input.Justification = "  " + input.Justification + "  "

	op := newAwaitingOperation(t, fixture, input)

	if op.Status != StatusAwaitingConfirmation {
		t.Fatalf("Status = %q", op.Status)
	}
	if op.DraftVersion != 1 {
		t.Fatalf("DraftVersion = %d", op.DraftVersion)
	}
	if op.Payload.Justification != strings.TrimSpace(input.Justification) {
		t.Fatalf("Justification = %q", op.Payload.Justification)
	}
	if op.Payload.ApproverID != fixture.Approver.UserID {
		t.Fatalf("ApproverID = %q", op.Payload.ApproverID)
	}
	if op.Ticket != nil {
		t.Fatal("new operation must not have a ticket")
	}
	if err := op.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := op.CheckConfirmation(1); err != nil {
		t.Fatalf("CheckConfirmation(1) error = %v", err)
	}

	var versionErr *DraftVersionConflictError
	err := op.CheckConfirmation(2)
	if !errors.As(err, &versionErr) {
		t.Fatalf("CheckConfirmation(2) error = %v, want DraftVersionConflictError", err)
	}
	if versionErr.Expected != 1 || versionErr.Actual != 2 {
		t.Fatalf("version error = %+v", versionErr)
	}
}

func TestNewAwaitingConfirmationDeniedByPolicy(t *testing.T) {
	fixture := newTestFixture(t, "space_goods_search_wiki")
	input := PayloadInput{
		ResourceID:    fixture.Resource.ID,
		AccessLevel:   AccessWrite,
		Justification: "Прохождение онбординга первой недели",
	}

	_, err := NewAwaitingConfirmation(
		"op_1", fixture.User, "tsk_901", fixture.Seed.Scenario.ID,
		input, fixture.Resource, fixture.Approver, testCreatedAt,
	)

	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %v, want AccessDeniedError", err)
	}
	if denied.Reason != DenyLevelNotInCatalog {
		t.Fatalf("Reason = %q", denied.Reason)
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error does not wrap ErrForbidden: %v", err)
	}
}

func TestOperationRevise(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	input := defaultPayload(fixture.Resource)
	op := newAwaitingOperation(t, fixture, input)

	changed, err := op.Revise(fixture.User, input, fixture.Resource, fixture.Approver, testUpdatedAt)
	if err != nil {
		t.Fatalf("Revise(same) error = %v", err)
	}
	if changed {
		t.Fatal("Revise(same) must not bump the draft version")
	}
	if op.DraftVersion != 1 {
		t.Fatalf("DraftVersion = %d", op.DraftVersion)
	}

	revised := input
	revised.AccessLevel = AccessWrite
	revised.Justification = "Нужен доступ для ведения заметок по онбордингу"
	changed, err = op.Revise(fixture.User, revised, fixture.Resource, fixture.Approver, testUpdatedAt)
	if err != nil {
		t.Fatalf("Revise(changed) error = %v", err)
	}
	if !changed {
		t.Fatal("Revise(changed) must bump the draft version")
	}
	if op.DraftVersion != 2 {
		t.Fatalf("DraftVersion = %d, want 2", op.DraftVersion)
	}
	if op.Payload.AccessLevel != AccessWrite {
		t.Fatalf("AccessLevel = %q", op.Payload.AccessLevel)
	}

	if err := op.BeginSubmission(testUpdatedAt); err != nil {
		t.Fatal(err)
	}
	_, err = op.Revise(fixture.User, revised, fixture.Resource, fixture.Approver, testUpdatedAt)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Revise(submitting) error = %v, want conflict", err)
	}
}

func TestOperationHappyPath(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))

	if err := op.BeginSubmission(testUpdatedAt); err != nil {
		t.Fatalf("BeginSubmission() error = %v", err)
	}
	if op.Status != StatusSubmitting {
		t.Fatalf("Status = %q", op.Status)
	}

	ticket := newTestTicket(t, op, TicketPending)
	if err := op.MarkSucceeded(ticket, testTicketAt); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if op.Status != StatusSucceeded {
		t.Fatalf("Status = %q", op.Status)
	}
	if op.Ticket == nil || op.Ticket.ID != ticket.ID {
		t.Fatalf("Ticket = %+v", op.Ticket)
	}
	if !op.Status.Terminal() {
		t.Fatal("succeeded status must be terminal")
	}
	if err := op.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if err := op.MarkSucceeded(ticket, testTicketAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("second MarkSucceeded error = %v, want conflict", err)
	}
}

func TestOperationRejectsForeignTicket(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
	if err := op.BeginSubmission(testUpdatedAt); err != nil {
		t.Fatal(err)
	}

	resourceID := op.Payload.ResourceID
	level := op.Payload.AccessLevel
	foreign, err := NewTicket(
		"tkt_999", "op_2", TicketPending, &resourceID, &level, nil, testTicketAt,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = op.MarkSucceeded(foreign, testTicketAt)
	if !errors.Is(err, ErrInvariant) {
		t.Fatalf("MarkSucceeded(foreign) error = %v, want invariant", err)
	}
	if op.Status != StatusSubmitting {
		t.Fatalf("Status changed to %q after rejected ticket", op.Status)
	}
}

func TestOperationUnknownReconcileFound(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
	if err := op.BeginSubmission(testUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := op.MarkUnknown(testUpdatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkUnknown() error = %v", err)
	}
	if op.Status != StatusUnknown {
		t.Fatalf("Status = %q", op.Status)
	}

	ticket := newTestTicket(t, op, TicketApproved)
	if err := op.ReconcileFound(ticket, testTicketAt); err != nil {
		t.Fatalf("ReconcileFound() error = %v", err)
	}
	if op.Status != StatusSucceeded || op.Ticket == nil {
		t.Fatalf("operation = %+v", op)
	}
}

func TestOperationUnknownReconcileNotFound(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
	if err := op.BeginSubmission(testUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := op.MarkUnknown(testUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := op.ReconcileNotFound(testUpdatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("ReconcileNotFound() error = %v", err)
	}
	if op.Status != StatusFailed {
		t.Fatalf("Status = %q", op.Status)
	}
	if !op.Status.Terminal() {
		t.Fatal("failed status must be terminal")
	}
}

func TestOperationExplicitFailure(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
	if err := op.BeginSubmission(testUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := op.MarkFailed(testUpdatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	if op.Status != StatusFailed {
		t.Fatalf("Status = %q", op.Status)
	}
	if err := op.MarkUnknown(testUpdatedAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("MarkUnknown(failed) error = %v, want conflict", err)
	}
}

func TestOperationInvalidTransitionsAndPayload(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")

	t.Run("cannot submit draft", func(t *testing.T) {
		op := Operation{
			ID:             "op_1",
			UserID:         fixture.User.UserID,
			TaskInstanceID: "tsk_901",
			ScenarioID:     fixture.Seed.Scenario.ID,
			Status:         StatusDraft,
			DraftVersion:   1,
			CreatedAt:      testCreatedAt,
			UpdatedAt:      testCreatedAt,
		}
		if err := op.Validate(); err != nil {
			t.Fatalf("draft Validate() error = %v", err)
		}
		if err := op.BeginSubmission(testUpdatedAt); !errors.Is(err, ErrConflict) {
			t.Fatalf("BeginSubmission(draft) error = %v, want conflict", err)
		}
	})

	t.Run("invalid justification", func(t *testing.T) {
		input := defaultPayload(fixture.Resource)
		input.Justification = "short"
		_, err := NewAwaitingConfirmation(
			"op_1", fixture.User, "tsk_901", fixture.Seed.Scenario.ID,
			input, fixture.Resource, fixture.Approver, testCreatedAt,
		)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want validation", err)
		}
	})

	t.Run("admin level is not requestable", func(t *testing.T) {
		resource := newTestFixture(t, "db_fintech_prod_cluster").Resource
		input := PayloadInput{
			ResourceID:    resource.ID,
			AccessLevel:   AccessAdmin,
			Justification: "Нужен доступ к продовому кластеру базы данных",
		}
		_, err := NewAwaitingConfirmation(
			"op_1", fixture.User, "tsk_901", fixture.Seed.Scenario.ID,
			input, resource, fixture.Approver, testCreatedAt,
		)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("error = %v, want policy denial", err)
		}
	})

	t.Run("approver must lead the user's team", func(t *testing.T) {
		approver := fixture.Approver
		approver.TeamID = "team_goods_search"
		_, err := NewAwaitingConfirmation(
			"op_1", fixture.User, "tsk_901", fixture.Seed.Scenario.ID,
			defaultPayload(fixture.Resource), fixture.Resource, approver, testCreatedAt,
		)
		if !errors.Is(err, ErrInvariant) {
			t.Fatalf("error = %v, want invariant", err)
		}
	})

	t.Run("succeeded without ticket is invalid", func(t *testing.T) {
		op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
		op.Status = StatusSucceeded
		if err := op.Validate(); !errors.Is(err, ErrInvariant) {
			t.Fatalf("Validate() error = %v, want invariant", err)
		}
	})
}
