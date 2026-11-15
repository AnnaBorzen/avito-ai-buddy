package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func baseAnalyticsEvent(event EventName) AnalyticsEvent {
	return AnalyticsEvent{
		EventID:         "evt_1",
		Event:           event,
		OccurredAt:      testCreatedAt,
		ActorRef:        "usr_intern_01",
		TaskInstanceID:  "tsk_901",
		ScenarioID:      "scn_team_space_access",
		ScenarioVersion: "1.0",
		Outcome:         OutcomeOK,
		TestMode:        true,
	}
}

func TestAnalyticsEventValidate(t *testing.T) {
	tests := []struct {
		name  string
		event AnalyticsEvent
	}{
		{
			name:  "task started",
			event: baseAnalyticsEvent(EventTaskStarted),
		},
		{
			name: "answer delivered",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventAnswerDelivered)
				e.ModelVersion = ptr("gpt-4.1-mini")
				e.SourceIDs = []string{}
				return e
			}(),
		},
		{
			name: "action proposed",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionProposed)
				e.OperationID = ptr(OperationID("op_1"))
				e.ResourceID = ptr(ResourceID("space_auto_backend_wiki"))
				e.AccessLevel = ptr(AccessWrite)
				return e
			}(),
		},
		{
			name: "action confirmed",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionConfirmed)
				e.OperationID = ptr(OperationID("op_1"))
				e.DraftVersion = ptr(1)
				return e
			}(),
		},
		{
			name: "action succeeded",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionSucceeded)
				e.OperationID = ptr(OperationID("op_1"))
				e.TicketID = ptr(TicketID("tkt_501"))
				e.DurationMS = ptr(1200)
				e.Recovered = ptr(false)
				return e
			}(),
		},
		{
			name: "action failed",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionFailed)
				e.Outcome = OutcomeError
				e.OperationID = ptr(OperationID("op_1"))
				e.ErrorCode = ptr("UPSTREAM_ERROR")
				e.ErrorCategory = ptr(ErrorCategoryUpstream)
				e.DurationMS = ptr(5000)
				e.Recovered = ptr(false)
				return e
			}(),
		},
		{
			name: "action failed without optional category",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionFailed)
				e.Outcome = OutcomeError
				e.OperationID = ptr(OperationID("op_1"))
				e.ErrorCode = ptr("UPSTREAM_ERROR")
				e.DurationMS = ptr(5000)
				e.Recovered = ptr(false)
				return e
			}(),
		},
		{
			name: "action status unknown",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionStatusUnknown)
				e.Outcome = OutcomeUnknown
				e.OperationID = ptr(OperationID("op_1"))
				e.ErrorCode = ptr("UPSTREAM_TIMEOUT")
				e.ErrorCategory = ptr(ErrorCategoryUpstreamTimeout)
				e.DurationMS = ptr(30000)
				return e
			}(),
		},
		{
			name: "handoff requested",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventHandoffRequested)
				e.Outcome = OutcomeDenied
				e.Reason = ptr(HandoffNoKnowledge)
				return e
			}(),
		},
		{
			name: "task feedback",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventTaskFeedbackSubmitted)
				e.Rating = ptr(5)
				return e
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestAnalyticsEventRejectsInvalidFacts(t *testing.T) {
	tests := []struct {
		name  string
		event AnalyticsEvent
	}{
		{
			name: "missing event id",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventTaskStarted)
				e.EventID = ""
				return e
			}(),
		},
		{
			name: "unsupported event",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventName("prompt_dumped"))
				return e
			}(),
		},
		{
			name: "missing outcome",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventTaskStarted)
				e.Outcome = ""
				return e
			}(),
		},
		{
			name: "answer without model",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventAnswerDelivered)
				e.SourceIDs = []string{}
				return e
			}(),
		},
		{
			name: "answer without sources presence",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventAnswerDelivered)
				e.ModelVersion = ptr("gpt-4.1-mini")
				return e
			}(),
		},
		{
			name: "proposed without operation",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionProposed)
				e.ResourceID = ptr(ResourceID("space_auto_backend_wiki"))
				e.AccessLevel = ptr(AccessRead)
				return e
			}(),
		},
		{
			name: "succeeded without ticket",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionSucceeded)
				e.OperationID = ptr(OperationID("op_1"))
				e.DurationMS = ptr(10)
				e.Recovered = ptr(false)
				return e
			}(),
		},
		{
			name: "succeeded without recovered flag",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionSucceeded)
				e.OperationID = ptr(OperationID("op_1"))
				e.TicketID = ptr(TicketID("tkt_501"))
				e.DurationMS = ptr(10)
				return e
			}(),
		},
		{
			name: "unknown without duration",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionStatusUnknown)
				e.OperationID = ptr(OperationID("op_1"))
				e.ErrorCode = ptr("UPSTREAM_TIMEOUT")
				e.ErrorCategory = ptr(ErrorCategoryUpstreamTimeout)
				return e
			}(),
		},

		{
			name: "handoff without reason",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventHandoffRequested)
				return e
			}(),
		},
		{
			name: "feedback without rating",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventTaskFeedbackSubmitted)
				return e
			}(),
		},
		{
			name: "negative duration",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventTaskStarted)
				e.DurationMS = ptr(-1)
				return e
			}(),
		},
		{
			name: "empty source id",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventAnswerDelivered)
				e.ModelVersion = ptr("gpt-4.1-mini")
				e.SourceIDs = []string{"doc_01", ""}
				return e
			}(),
		},
		{
			name: "invalid access level",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionProposed)
				e.OperationID = ptr(OperationID("op_1"))
				e.ResourceID = ptr(ResourceID("space_auto_backend_wiki"))
				e.AccessLevel = ptr(AccessLevel("superuser"))
				return e
			}(),
		},
		{
			name: "zero draft version",
			event: func() AnalyticsEvent {
				e := baseAnalyticsEvent(EventActionConfirmed)
				e.OperationID = ptr(OperationID("op_1"))
				e.DraftVersion = ptr(0)
				return e
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestAnalyticsEventHasNoFreeFormOrSensitiveFields(t *testing.T) {
	typ := reflect.TypeOf(AnalyticsEvent{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.ToLower(field.Name)
		for _, forbidden := range []string{"message", "prompt", "token", "attribute"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("field %q looks like sensitive or free-form analytics data", field.Name)
			}
		}
		if field.Type.Kind() == reflect.Map {
			t.Fatalf("field %q must not be a map", field.Name)
		}
	}
}

func TestAnalyticsEventOccurredAtMustBeSet(t *testing.T) {
	event := baseAnalyticsEvent(EventTaskStarted)
	event.OccurredAt = time.Time{}
	if err := event.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("Validate() error = %v, want validation error", err)
	}
}
