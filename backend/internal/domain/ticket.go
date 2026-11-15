package domain

import (
	"fmt"
	"time"
)

// TicketStatus mirrors the external access-service status. The backend never
// invents additional values such as "provisioned".
type TicketStatus string

const (
	TicketPending  TicketStatus = "pending"
	TicketApproved TicketStatus = "approved"
	TicketRejected TicketStatus = "rejected"
)

func (s TicketStatus) Valid() bool {
	switch s {
	case TicketPending, TicketApproved, TicketRejected:
		return true
	default:
		return false
	}
}

// Ticket is an immutable snapshot of an access-service ticket.
type Ticket struct {
	ID             TicketID
	OperationID    OperationID
	ExternalStatus TicketStatus
	ResourceID     *ResourceID
	AccessLevel    *AccessLevel
	ApproverName   *string
	CreatedAt      time.Time
}

// NewTicket validates and copies optional values, preventing callers from
// mutating a persisted ticket through shared pointers.
func NewTicket(
	id TicketID,
	operationID OperationID,
	status TicketStatus,
	resourceID *ResourceID,
	accessLevel *AccessLevel,
	approverName *string,
	createdAt time.Time,
) (Ticket, error) {
	ticket := Ticket{
		ID:             id,
		OperationID:    operationID,
		ExternalStatus: status,
		ResourceID:     copyResourceID(resourceID),
		AccessLevel:    copyAccessLevel(accessLevel),
		ApproverName:   copyString(approverName),
		CreatedAt:      createdAt.UTC(),
	}
	if err := ticket.Validate(); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

func (t Ticket) Validate() error {
	if err := t.ID.Validate(); err != nil {
		return err
	}
	if err := t.OperationID.Validate(); err != nil {
		return err
	}
	if !t.ExternalStatus.Valid() {
		return &ValidationError{
			Field:   "ticket.external_status",
			Message: fmt.Sprintf("unsupported status %q", t.ExternalStatus),
		}
	}
	if t.ResourceID != nil {
		if err := t.ResourceID.Validate(); err != nil {
			return err
		}
	}
	if t.AccessLevel != nil && !t.AccessLevel.Valid() {
		return &ValidationError{
			Field:   "ticket.access_level",
			Message: fmt.Sprintf("unsupported level %q", *t.AccessLevel),
		}
	}
	if t.ApproverName != nil {
		if err := requireNonEmpty("ticket.approver_name", *t.ApproverName); err != nil {
			return err
		}
	}
	if t.CreatedAt.IsZero() {
		return &ValidationError{Field: "ticket.created_at", Message: "must be set"}
	}
	return nil
}

func copyResourceID(value *ResourceID) *ResourceID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyAccessLevel(value *AccessLevel) *AccessLevel {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
