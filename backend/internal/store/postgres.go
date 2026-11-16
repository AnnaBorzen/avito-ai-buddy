// Package store persists backend-owned tasks, operations and analytics.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
)

// ErrNotFound is returned when the scoped record does not exist.
var ErrNotFound = errors.New("store: not found")

// Task is the durable chat task state owned by the backend.
type Task struct {
	ID             domain.TaskInstanceID
	UserID         domain.UserID
	ScenarioID     domain.ScenarioID
	RulesExplained bool
	ResourceID     *string
	AccessLevel    *string
	Justification  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SourceSnapshot is the persisted source of an assistant message.
type SourceSnapshot struct {
	DocID       string
	Title       string
	Section     string
	Excerpt     string
	IsSynthetic bool
}

// Message is one durable chat message.
type Message struct {
	ID        string
	Role      string
	Content   string
	Source    *SourceSnapshot
	Handoff   bool
	CreatedAt time.Time
}

// Store wraps a PostgreSQL pool. The pool is safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// Open creates and verifies a connection pool. PostgreSQL is the only
// persistence backend of the service; there is no demo/in-memory fallback.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: configure pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("store: ping: %w", err)
	}
	return nil
}

// EnsureTask returns the user's task, creating it on first contact. A task is
// the durable unit of analytics and of the chat scenario.
func (s *Store) EnsureTask(ctx context.Context, userID domain.UserID, scenarioID domain.ScenarioID) (Task, bool, error) {
	row := s.pool.QueryRow(ctx, `
		WITH next AS (SELECT 'tsk_' || nextval('task_instance_seq') AS id)
		INSERT INTO tasks (task_instance_id, user_id, scenario_id)
		SELECT id, $1, $2 FROM next
		ON CONFLICT (user_id) DO UPDATE SET updated_at = tasks.updated_at
		RETURNING task_instance_id, user_id, scenario_id, rules_explained,
		          resource_id, access_level, justification, created_at, updated_at,
		          (xmax = 0) AS created`, userID, scenarioID)

	var task Task
	var created bool
	if err := row.Scan(
		&task.ID, &task.UserID, &task.ScenarioID, &task.RulesExplained,
		&task.ResourceID, &task.AccessLevel, &task.Justification,
		&task.CreatedAt, &task.UpdatedAt, &created,
	); err != nil {
		return Task{}, false, fmt.Errorf("store: ensure task: %w", err)
	}
	return task, created, nil
}

func (s *Store) Task(ctx context.Context, userID domain.UserID) (Task, error) {
	var task Task
	err := s.pool.QueryRow(ctx, `
		SELECT task_instance_id, user_id, scenario_id, rules_explained,
		       resource_id, access_level, justification, created_at, updated_at
		FROM tasks WHERE user_id = $1`, userID).Scan(
		&task.ID, &task.UserID, &task.ScenarioID, &task.RulesExplained,
		&task.ResourceID, &task.AccessLevel, &task.Justification,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("store: load task: %w", err)
	}
	return task, nil
}

// UpdateTaskSlots persists only the three draft slots, leaving ownership and
// timestamps under the database's control.
func (s *Store) UpdateTaskSlots(ctx context.Context, id domain.TaskInstanceID, resourceID, level, justification *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tasks
		SET resource_id = $2, access_level = $3, justification = $4
		WHERE task_instance_id = $1`, id, resourceID, level, justification)
	if err != nil {
		return fmt.Errorf("store: update task slots: %w", err)
	}
	return nil
}

func (s *Store) MarkRulesExplained(ctx context.Context, id domain.TaskInstanceID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tasks SET rules_explained = true
		WHERE task_instance_id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: mark rules explained: %w", err)
	}
	return nil
}

func (s *Store) AddMessage(ctx context.Context, taskID domain.TaskInstanceID, msg Message) error {
	var docID, title, section, excerpt *string
	var synthetic *bool
	if msg.Source != nil {
		docID, title = &msg.Source.DocID, &msg.Source.Title
		section, excerpt = &msg.Source.Section, &msg.Source.Excerpt
		synthetic = &msg.Source.IsSynthetic
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO task_messages (
			message_id, task_instance_id, role, content,
			source_doc_id, source_title, source_section, source_excerpt, source_synthetic, handoff
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		msg.ID, taskID, msg.Role, msg.Content, docID, title, section, excerpt, synthetic, msg.Handoff)
	if err != nil {
		return fmt.Errorf("store: add message: %w", err)
	}
	return nil
}

func (s *Store) Messages(ctx context.Context, taskID domain.TaskInstanceID) ([]Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT message_id, role, content, source_doc_id, source_title, source_section,
		       source_excerpt, source_synthetic, handoff, created_at
		FROM task_messages
		WHERE task_instance_id = $1
		ORDER BY created_at, message_id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var docID, title, section, excerpt *string
		var synthetic *bool
		if err := rows.Scan(
			&msg.ID, &msg.Role, &msg.Content, &docID, &title, &section,
			&excerpt, &synthetic, &msg.Handoff, &msg.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan message: %w", err)
		}
		if docID != nil && title != nil && section != nil && excerpt != nil && synthetic != nil {
			msg.Source = &SourceSnapshot{
				DocID: *docID, Title: *title, Section: *section,
				Excerpt: *excerpt, IsSynthetic: *synthetic,
			}
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	return messages, nil
}

func (s *Store) NextOperationID(ctx context.Context) (domain.OperationID, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT 'op_' || nextval('operation_id_seq')`).Scan(&id); err != nil {
		return "", fmt.Errorf("store: next operation id: %w", err)
	}
	return domain.OperationID(id), nil
}

func (s *Store) CreateOperation(ctx context.Context, op domain.Operation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin operation create: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	payload := op.Payload
	_, err = tx.Exec(ctx, `
		INSERT INTO operations (
			operation_id, user_id, task_instance_id, scenario_id, status, draft_version,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		op.ID, op.UserID, op.TaskInstanceID, op.ScenarioID, op.Status, op.DraftVersion,
		op.CreatedAt, op.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: create operation: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO operation_payloads (
			operation_id, resource_id, resource_title, access_level, justification,
			approver_id, approver_name
		) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		op.ID, payload.ResourceID, payload.ResourceTitle, payload.AccessLevel,
		payload.Justification, payload.ApproverID, payload.ApproverName)
	if err != nil {
		return fmt.Errorf("store: create operation payload: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit operation create: %w", err)
	}
	return nil
}

// UpdateOperation writes the mutable operation state. The payload is part of
// the draft and may change only before submission; the ticket is written once.
func (s *Store) UpdateOperation(ctx context.Context, op domain.Operation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin operation update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	payload := op.Payload
	tag, err := tx.Exec(ctx, `
		UPDATE operations
		SET status = $2, draft_version = $3, updated_at = $4
		WHERE operation_id = $1`,
		op.ID, op.Status, op.DraftVersion, op.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: update operation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = tx.Exec(ctx, `
		UPDATE operation_payloads
		SET resource_id = $2, resource_title = $3, access_level = $4,
		    justification = $5, approver_id = $6, approver_name = $7
		WHERE operation_id = $1`,
		op.ID, payload.ResourceID, payload.ResourceTitle, payload.AccessLevel,
		payload.Justification, payload.ApproverID, payload.ApproverName)
	if err != nil {
		return fmt.Errorf("store: update operation payload: %w", err)
	}
	if op.Ticket != nil {
		ticket := op.Ticket
		_, err = tx.Exec(ctx, `
			INSERT INTO tickets (
				ticket_id, operation_id, external_status, resource_id,
				access_level, approver_name, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (operation_id) DO NOTHING`,
			ticket.ID, ticket.OperationID, ticket.ExternalStatus, ticket.ResourceID,
			ticket.AccessLevel, ticket.ApproverName, ticket.CreatedAt)
		if err != nil {
			return fmt.Errorf("store: save ticket: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit operation update: %w", err)
	}
	return nil
}

// Operation loads one operation scoped to its owner. Cross-user access is
// reported as not found, not as forbidden (FR-09).
func (s *Store) Operation(ctx context.Context, userID domain.UserID, id domain.OperationID) (domain.Operation, error) {
	return s.loadOperation(ctx, "o.user_id = $1 AND o.operation_id = $2", userID, id)
}

func (s *Store) LatestOperation(ctx context.Context, userID domain.UserID) (*domain.Operation, error) {
	op, err := s.loadOperation(ctx, "o.user_id = $1 ORDER BY o.created_at DESC, o.operation_id DESC LIMIT 1", userID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (s *Store) loadOperation(ctx context.Context, where string, args ...any) (domain.Operation, error) {
	query := `
		SELECT o.operation_id, o.user_id, o.task_instance_id, o.scenario_id,
		       o.status, o.draft_version, o.created_at, o.updated_at,
		       p.resource_id, p.resource_title, p.access_level, p.justification,
		       p.approver_id, p.approver_name,
		       t.ticket_id, t.external_status, t.resource_id, t.access_level,
		       t.approver_name, t.created_at
		FROM operations o
		JOIN operation_payloads p ON p.operation_id = o.operation_id
		LEFT JOIN tickets t ON t.operation_id = o.operation_id
		WHERE ` + where

	var op domain.Operation
	var ticketID, ticketStatus, ticketResource, ticketLevel, ticketApprover *string
	var ticketCreated *time.Time
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&op.ID, &op.UserID, &op.TaskInstanceID, &op.ScenarioID,
		&op.Status, &op.DraftVersion, &op.CreatedAt, &op.UpdatedAt,
		&op.Payload.ResourceID, &op.Payload.ResourceTitle, &op.Payload.AccessLevel,
		&op.Payload.Justification, &op.Payload.ApproverID, &op.Payload.ApproverName,
		&ticketID, &ticketStatus, &ticketResource, &ticketLevel,
		&ticketApprover, &ticketCreated,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, ErrNotFound
	}
	if err != nil {
		return domain.Operation{}, fmt.Errorf("store: load operation: %w", err)
	}
	if ticketID == nil || ticketStatus == nil || ticketCreated == nil {
		return op, nil
	}
	var resource *domain.ResourceID
	if ticketResource != nil {
		value := domain.ResourceID(*ticketResource)
		resource = &value
	}
	var level *domain.AccessLevel
	if ticketLevel != nil {
		value := domain.AccessLevel(*ticketLevel)
		level = &value
	}
	ticket, err := domain.NewTicket(
		domain.TicketID(*ticketID), op.ID, domain.TicketStatus(*ticketStatus),
		resource, level, ticketApprover, *ticketCreated,
	)
	if err != nil {
		return domain.Operation{}, fmt.Errorf("store: restore ticket: %w", err)
	}
	op.Ticket = &ticket
	return op, nil
}

// TicketHistory returns the user's prior ticket snapshots, newest first.
func (s *Store) TicketHistory(ctx context.Context, userID domain.UserID, limit int) ([]domain.Ticket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.ticket_id, t.operation_id, t.external_status, t.resource_id,
		       t.access_level, t.approver_name, t.created_at
		FROM tickets t
		JOIN operations o ON o.operation_id = t.operation_id
		WHERE o.user_id = $1
		ORDER BY t.created_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: ticket history: %w", err)
	}
	defer rows.Close()

	var tickets []domain.Ticket
	for rows.Next() {
		var ticketID, opID, status string
		var resource, level, approver *string
		var created time.Time
		if err := rows.Scan(&ticketID, &opID, &status, &resource, &level, &approver, &created); err != nil {
			return nil, fmt.Errorf("store: scan ticket: %w", err)
		}
		var resourceID *domain.ResourceID
		if resource != nil {
			value := domain.ResourceID(*resource)
			resourceID = &value
		}
		var accessLevel *domain.AccessLevel
		if level != nil {
			value := domain.AccessLevel(*level)
			accessLevel = &value
		}
		ticket, err := domain.NewTicket(
			domain.TicketID(ticketID), domain.OperationID(opID),
			domain.TicketStatus(status), resourceID, accessLevel, approver, created,
		)
		if err != nil {
			return nil, fmt.Errorf("store: restore ticket history: %w", err)
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: ticket history: %w", err)
	}
	return tickets, nil
}

// Idempotency loads a key scoped to user and operation.
func (s *Store) Idempotency(ctx context.Context, userID domain.UserID, operationID domain.OperationID, key domain.IdempotencyKey) (*domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord
	var status *int
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, operation_id, key, request_hash, response_status, response_body, created_at
		FROM idempotency_keys
		WHERE user_id = $1 AND operation_id = $2 AND key = $3`,
		userID, operationID, key,
	).Scan(&record.UserID, &record.OperationID, &record.Key, &record.RequestHash,
		&status, &record.ResponseBody, &record.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load idempotency key: %w", err)
	}
	record.ResponseStatus = status
	return &record, nil
}

// ReserveIdempotency inserts a pending key. false means another request owns it.
func (s *Store) ReserveIdempotency(ctx context.Context, record domain.IdempotencyRecord) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO idempotency_keys (user_id, operation_id, key, request_hash, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id, operation_id, key) DO NOTHING`,
		record.UserID, record.OperationID, record.Key, record.RequestHash, record.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("store: reserve idempotency key: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) CompleteIdempotency(ctx context.Context, userID domain.UserID, operationID domain.OperationID, key domain.IdempotencyKey, status int, body []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE idempotency_keys
		SET response_status = $4, response_body = $5
		WHERE user_id = $1 AND operation_id = $2 AND key = $3`,
		userID, operationID, key, status, body)
	if err != nil {
		return fmt.Errorf("store: complete idempotency key: %w", err)
	}
	return nil
}

func (s *Store) DeleteIdempotency(ctx context.Context, userID domain.UserID, operationID domain.OperationID, key domain.IdempotencyKey) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM idempotency_keys
		WHERE user_id = $1 AND operation_id = $2 AND key = $3`,
		userID, operationID, key)
	if err != nil {
		return fmt.Errorf("store: delete idempotency key: %w", err)
	}
	return nil
}

// Feedback returns the saved rating for a task; nil means no rating yet.
func (s *Store) Feedback(ctx context.Context, taskID domain.TaskInstanceID) (*int, error) {
	var rating *int
	err := s.pool.QueryRow(ctx, `
		SELECT rating FROM analytics_events
		WHERE task_instance_id = $1 AND event = 'task_feedback_submitted'
		ORDER BY occurred_at DESC LIMIT 1`, taskID).Scan(&rating)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load feedback: %w", err)
	}
	return rating, nil
}

// InsertEvent persists an analytics fact. Analytics failures must not fail the
// user scenario, so callers log the error instead of rolling back.
func (s *Store) InsertEvent(ctx context.Context, event domain.AnalyticsEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO analytics_events (
			event_id, event, occurred_at, actor_ref, task_instance_id, scenario_id,
			scenario_version, operation_id, outcome, error_category, error_code,
			duration_ms, model_version, test_mode, source_ids, resource_id,
			access_level, draft_version, ticket_id, recovered, reason, rating
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		event.EventID, event.Event, event.OccurredAt, event.ActorRef,
		event.TaskInstanceID, event.ScenarioID, event.ScenarioVersion,
		event.OperationID, event.Outcome, event.ErrorCategory, event.ErrorCode,
		event.DurationMS, event.ModelVersion, event.TestMode, event.SourceIDs,
		event.ResourceID, event.AccessLevel, event.DraftVersion, event.TicketID,
		event.Recovered, event.Reason, event.Rating)
	if err != nil {
		return fmt.Errorf("store: insert analytics event: %w", err)
	}
	return nil
}
