package service

import (
	"errors"
	"fmt"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
)

var (
	// ErrTaskNotFound means task_instance_id does not belong to this user.
	ErrTaskNotFound = errors.New("service: task not found")
	// ErrUpstreamTimeout means the access service may have processed the
	// request; the operation is moved to "unknown".
	ErrUpstreamTimeout = errors.New("service: access service timeout")
	// ErrUpstreamError means the access service explicitly failed the request.
	ErrUpstreamError = errors.New("service: access service error")
	// ErrIdempotencyInProgress means another request holds the same key.
	ErrIdempotencyInProgress = errors.New("service: idempotent request in progress")
	// ErrIdempotencyKeyReused means the key was reused for another payload.
	ErrIdempotencyKeyReused = errors.New("service: idempotency key reused with another payload")
	// ErrOperationAlreadyFinished means the operation can no longer be sent.
	ErrOperationAlreadyFinished = errors.New("service: operation already finished")
)

// UpstreamError carries the operation state alongside the upstream failure so
// the transport can return operation_id and operation_status in ApiError.
type UpstreamError struct {
	Kind      error
	Operation domain.OperationID
	Status    domain.OperationStatus
	Cause     error
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("service: upstream %v for operation %s (%s): %v",
		e.Kind, e.Operation, e.Status, e.Cause)
}

func (e *UpstreamError) Unwrap() error { return e.Kind }

// RbacDenial builds the normal conversational denial from a domain decision.
func RbacDenialFrom(err *domain.AccessDeniedError) *RbacDenial {
	return &RbacDenial{
		ErrorCode: "RBAC_ACCESS_DENIED",
		Message: fmt.Sprintf(
			"Роли '%s' запрещено запрашивать уровень '%s' к ресурсу '%s'.",
			err.Role, err.AccessLevel, err.ResourceID,
		),
		ResourceID:  string(err.ResourceID),
		AccessLevel: string(err.AccessLevel),
	}
}
