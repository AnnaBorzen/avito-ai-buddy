package domain

import (
	"fmt"
	"time"
)

// EventName is the fixed analytics event vocabulary from docs/metrics.md.
type EventName string

const (
	EventTaskStarted           EventName = "task_started"
	EventAnswerDelivered       EventName = "answer_delivered"
	EventActionProposed        EventName = "action_proposed"
	EventActionConfirmed       EventName = "action_confirmed"
	EventActionSucceeded       EventName = "action_succeeded"
	EventActionFailed          EventName = "action_failed"
	EventActionStatusUnknown   EventName = "action_status_unknown"
	EventHandoffRequested      EventName = "handoff_requested"
	EventTaskFeedbackSubmitted EventName = "task_feedback_submitted"
)

func (e EventName) Valid() bool {
	switch e {
	case EventTaskStarted,
		EventAnswerDelivered,
		EventActionProposed,
		EventActionConfirmed,
		EventActionSucceeded,
		EventActionFailed,
		EventActionStatusUnknown,
		EventHandoffRequested,
		EventTaskFeedbackSubmitted:
		return true
	default:
		return false
	}
}

type Outcome string

const (
	OutcomeOK      Outcome = "ok"
	OutcomeDenied  Outcome = "denied"
	OutcomeError   Outcome = "error"
	OutcomeUnknown Outcome = "unknown"
)

func (o Outcome) Valid() bool {
	switch o {
	case OutcomeOK, OutcomeDenied, OutcomeError, OutcomeUnknown:
		return true
	default:
		return false
	}
}

type ErrorCategory string

const (
	ErrorCategoryRBAC            ErrorCategory = "rbac"
	ErrorCategoryValidation      ErrorCategory = "validation"
	ErrorCategoryUpstreamTimeout ErrorCategory = "upstream_timeout"
	ErrorCategoryUpstream        ErrorCategory = "upstream_error"
	ErrorCategoryInternal        ErrorCategory = "internal"
)

func (c ErrorCategory) Valid() bool {
	switch c {
	case ErrorCategoryRBAC,
		ErrorCategoryValidation,
		ErrorCategoryUpstreamTimeout,
		ErrorCategoryUpstream,
		ErrorCategoryInternal:
		return true
	default:
		return false
	}
}

type HandoffReason string

const (
	HandoffNoKnowledge   HandoffReason = "no_knowledge"
	HandoffUserInitiated HandoffReason = "user_initiated"
)

func (r HandoffReason) Valid() bool {
	switch r {
	case HandoffNoKnowledge, HandoffUserInitiated:
		return true
	default:
		return false
	}
}

// AnalyticsEvent is a structured analytics fact. There is intentionally no
// free-form attributes map, message text or token field: such data must never
// reach analytics. Optional fields use pointers so absence differs from false,
// zero and an empty source list.
type AnalyticsEvent struct {
	EventID         EventID
	Event           EventName
	OccurredAt      time.Time
	ActorRef        UserID
	TaskInstanceID  TaskInstanceID
	ScenarioID      ScenarioID
	ScenarioVersion string

	OperationID *OperationID
	Outcome     Outcome

	ErrorCategory *ErrorCategory
	ErrorCode     *string
	DurationMS    *int
	ModelVersion  *string
	TestMode      bool

	SourceIDs    []string
	ResourceID   *ResourceID
	AccessLevel  *AccessLevel
	DraftVersion *int
	TicketID     *TicketID
	Recovered    *bool
	Reason       *HandoffReason
	Rating       *int
}

func (e AnalyticsEvent) Validate() error {
	if err := e.EventID.Validate(); err != nil {
		return err
	}
	if !e.Event.Valid() {
		return &ValidationError{Field: "event", Message: fmt.Sprintf("unsupported event %q", e.Event)}
	}
	if e.OccurredAt.IsZero() {
		return &ValidationError{Field: "occurred_at", Message: "must be set"}
	}
	if err := e.ActorRef.Validate(); err != nil {
		return err
	}
	if err := e.TaskInstanceID.Validate(); err != nil {
		return err
	}
	if err := e.ScenarioID.Validate(); err != nil {
		return err
	}
	if err := requireNonEmpty("scenario_version", e.ScenarioVersion); err != nil {
		return err
	}
	if !e.Outcome.Valid() {
		return &ValidationError{Field: "outcome", Message: fmt.Sprintf("unsupported outcome %q", e.Outcome)}
	}
	if e.ErrorCategory != nil && !e.ErrorCategory.Valid() {
		return &ValidationError{
			Field:   "error_category",
			Message: fmt.Sprintf("unsupported category %q", *e.ErrorCategory),
		}
	}
	if e.OperationID != nil {
		if err := e.OperationID.Validate(); err != nil {
			return err
		}
	}
	if e.ErrorCode != nil {
		if err := requireNonEmpty("error_code", *e.ErrorCode); err != nil {
			return err
		}
	}
	if e.DurationMS != nil && *e.DurationMS < 0 {
		return &ValidationError{Field: "duration_ms", Message: "must not be negative"}
	}
	if e.ModelVersion != nil {
		if err := requireNonEmpty("model_version", *e.ModelVersion); err != nil {
			return err
		}
	}
	if e.SourceIDs != nil {
		for i, sourceID := range e.SourceIDs {
			if sourceID == "" {
				return &ValidationError{
					Field:   "source_ids",
					Message: fmt.Sprintf("element %d must not be empty", i),
				}
			}
		}
	}
	if e.ResourceID != nil {
		if err := e.ResourceID.Validate(); err != nil {
			return err
		}
	}
	if e.AccessLevel != nil && !e.AccessLevel.Valid() {
		return &ValidationError{
			Field:   "access_level",
			Message: fmt.Sprintf("unsupported level %q", *e.AccessLevel),
		}
	}
	if e.DraftVersion != nil && *e.DraftVersion < 1 {
		return &ValidationError{Field: "draft_version", Message: "must be at least 1"}
	}
	if e.TicketID != nil {
		if err := e.TicketID.Validate(); err != nil {
			return err
		}
	}
	if e.Reason != nil && !e.Reason.Valid() {
		return &ValidationError{Field: "reason", Message: fmt.Sprintf("unsupported reason %q", *e.Reason)}
	}
	if e.Rating != nil && *e.Rating < 0 {
		return &ValidationError{Field: "rating", Message: "must not be negative"}
	}

	return e.validateEventRequirements()
}

func (e AnalyticsEvent) validateEventRequirements() error {
	switch e.Event {
	case EventAnswerDelivered:
		if e.ModelVersion == nil {
			return requiredFor("model_version", e.Event)
		}
		// A non-nil empty slice means "answer without sources" and is valid.
		if e.SourceIDs == nil {
			return requiredFor("source_ids", e.Event)
		}
	case EventActionProposed:
		if e.OperationID == nil {
			return requiredFor("operation_id", e.Event)
		}
		if e.ResourceID == nil {
			return requiredFor("resource_id", e.Event)
		}
		if e.AccessLevel == nil {
			return requiredFor("access_level", e.Event)
		}
	case EventActionConfirmed:
		if e.OperationID == nil {
			return requiredFor("operation_id", e.Event)
		}
		if e.DraftVersion == nil {
			return requiredFor("draft_version", e.Event)
		}
	case EventActionSucceeded:
		if e.OperationID == nil {
			return requiredFor("operation_id", e.Event)
		}
		if e.TicketID == nil {
			return requiredFor("ticket_id", e.Event)
		}
		if e.DurationMS == nil {
			return requiredFor("duration_ms", e.Event)
		}
		if e.Recovered == nil {
			return requiredFor("recovered", e.Event)
		}
	case EventActionFailed:
		if e.OperationID == nil {
			return requiredFor("operation_id", e.Event)
		}
		if e.ErrorCode == nil {
			return requiredFor("error_code", e.Event)
		}
		if e.DurationMS == nil {
			return requiredFor("duration_ms", e.Event)
		}
		if e.Recovered == nil {
			return requiredFor("recovered", e.Event)
		}
	case EventActionStatusUnknown:
		if e.OperationID == nil {
			return requiredFor("operation_id", e.Event)
		}
		if e.ErrorCode == nil {
			return requiredFor("error_code", e.Event)
		}
		if e.DurationMS == nil {
			return requiredFor("duration_ms", e.Event)
		}
	case EventHandoffRequested:
		if e.Reason == nil {
			return requiredFor("reason", e.Event)
		}
	case EventTaskFeedbackSubmitted:
		if e.Rating == nil {
			return requiredFor("rating", e.Event)
		}
	}
	return nil
}

func requiredFor(field string, event EventName) error {
	return &ValidationError{
		Field:   field,
		Message: fmt.Sprintf("is required for event %q", event),
	}
}
