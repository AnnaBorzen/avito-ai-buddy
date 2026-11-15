package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors let callers classify failures without matching messages.
var (
	ErrNotFound   = errors.New("domain: not found")
	ErrForbidden  = errors.New("domain: forbidden")
	ErrConflict   = errors.New("domain: conflict")
	ErrValidation = errors.New("domain: validation")
	ErrInvariant  = errors.New("domain: invariant violated")
)

// ValidationError describes an invalid input field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("domain: invalid %s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error { return ErrValidation }

// NotFoundError describes a missing reference-data entity.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("domain: %s not found", e.Entity)
	}
	return fmt.Sprintf("domain: %s %q not found", e.Entity, e.ID)
}

func (e *NotFoundError) Unwrap() error { return ErrNotFound }

// ConflictError describes a state transition that is not allowed right now.
type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string {
	return "domain: conflict: " + e.Message
}

func (e *ConflictError) Unwrap() error { return ErrConflict }

// DraftVersionConflictError carries both versions so the transport layer can
// return the current draft to the client.
type DraftVersionConflictError struct {
	Expected int
	Actual   int
}

func (e *DraftVersionConflictError) Error() string {
	return fmt.Sprintf("domain: draft version conflict: expected %d, actual %d", e.Expected, e.Actual)
}

func (e *DraftVersionConflictError) Unwrap() error { return ErrConflict }

// AccessDeniedError is a policy decision, not a malformed request.
type AccessDeniedError struct {
	UserID      UserID
	Role        Role
	ResourceID  ResourceID
	AccessLevel AccessLevel
	Reason      DenyReason
}

func (e *AccessDeniedError) Error() string {
	return fmt.Sprintf(
		"domain: access denied for user %q to resource %q level %q: %s",
		e.UserID, e.ResourceID, e.AccessLevel, e.Reason,
	)
}

func (e *AccessDeniedError) Unwrap() error { return ErrForbidden }

// InvariantError signals a programming defect or corrupted persisted state.
type InvariantError struct {
	Message string
}

func (e *InvariantError) Error() string {
	return "domain: invariant violated: " + e.Message
}

func (e *InvariantError) Unwrap() error { return ErrInvariant }
