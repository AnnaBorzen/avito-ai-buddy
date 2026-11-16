package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AccessTicket is the access service's own record of a request.
type AccessTicket struct {
	TicketID       string
	OperationID    string
	UserID         string
	TeamID         string
	ResourceID     string
	AccessLevel    string
	Justification  string
	ApproverID     string
	ApproverName   string
	ExternalStatus string
	CreatedAt      time.Time
}

// ErrTicketConflict means operation_id was already used with other data.
var ErrTicketConflict = errors.New("store: operation_id already has different ticket data")

// CreateAccessTicket inserts the ticket once per operation. created=false means
// the same operation_id was submitted before and the existing record is returned.
func (s *Store) CreateAccessTicket(ctx context.Context, in AccessTicket) (AccessTicket, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AccessTicket{}, false, fmt.Errorf("store: begin ticket: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	tag, err := tx.Exec(ctx, `
		INSERT INTO access_service_tickets (
			operation_id, ticket_id, user_id, team_id, resource_id, access_level,
			justification, approver_id, approver_name, external_status
		) VALUES (
			$1,
			'REQ-' || to_char(now(), 'YYYY') || '-' || lpad(nextval('access_ticket_seq')::text, 5, '0'),
			$2,$3,$4,$5,$6,$7,$8,'pending'
		)
		ON CONFLICT (operation_id) DO NOTHING`,
		in.OperationID, in.UserID, in.TeamID, in.ResourceID, in.AccessLevel,
		in.Justification, in.ApproverID, in.ApproverName)
	if err != nil {
		return AccessTicket{}, false, fmt.Errorf("store: insert access ticket: %w", err)
	}
	created := tag.RowsAffected() == 1

	ticket, err := loadAccessTicket(ctx, tx, in.OperationID)
	if err != nil {
		return AccessTicket{}, false, err
	}
	if !created {
		if ticket.UserID != in.UserID || ticket.TeamID != in.TeamID ||
			ticket.ResourceID != in.ResourceID || ticket.AccessLevel != in.AccessLevel ||
			ticket.Justification != in.Justification || ticket.ApproverID != in.ApproverID {
			return AccessTicket{}, false, ErrTicketConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AccessTicket{}, false, fmt.Errorf("store: commit ticket: %w", err)
	}
	return ticket, created, nil
}

// FindAccessTicket returns the access service's ticket or nil.
func (s *Store) FindAccessTicket(ctx context.Context, operationID string) (*AccessTicket, error) {
	ticket, err := loadAccessTicket(ctx, s.pool, operationID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

type ticketQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadAccessTicket(ctx context.Context, q ticketQuerier, operationID string) (AccessTicket, error) {
	var ticket AccessTicket
	err := q.QueryRow(ctx, `
		SELECT ticket_id, operation_id, user_id, team_id, resource_id, access_level,
		       justification, approver_id, approver_name, external_status, created_at
		FROM access_service_tickets
		WHERE operation_id = $1`, operationID).Scan(
		&ticket.TicketID, &ticket.OperationID, &ticket.UserID, &ticket.TeamID,
		&ticket.ResourceID, &ticket.AccessLevel, &ticket.Justification,
		&ticket.ApproverID, &ticket.ApproverName, &ticket.ExternalStatus, &ticket.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessTicket{}, ErrNotFound
	}
	if err != nil {
		return AccessTicket{}, fmt.Errorf("store: load access ticket: %w", err)
	}
	return ticket, nil
}

// AccessReady checks that the access-service tables are reachable.
func (s *Store) AccessReady(ctx context.Context) error {
	var one int
	if err := s.pool.QueryRow(ctx, `SELECT 1 FROM access_service_tickets LIMIT 1`).Scan(&one); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("store: access readiness: %w", err)
	}
	return nil
}
