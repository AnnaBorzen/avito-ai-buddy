package domain

import (
	"strings"
	"testing"
	"time"
)

func projectTestScenario(t *testing.T, fixture testFixture, rules bool, op *Operation) Scenario {
	t.Helper()
	scenario, err := ProjectScenario(fixture.Seed.Scenario, ScenarioInput{
		TaskInstanceID: "tsk_901",
		RulesExplained: rules,
		Operation:      op,
	})
	if err != nil {
		t.Fatalf("ProjectScenario() error = %v", err)
	}
	return scenario
}

func stepStatus(t *testing.T, scenario Scenario, id string) StepStatus {
	t.Helper()
	for _, step := range scenario.Steps {
		if step.ID == id {
			return step.Status
		}
	}
	t.Fatalf("step %q not found", id)
	return ""
}

func TestProjectScenarioWithoutOperation(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")

	scenario := projectTestScenario(t, fixture, false, nil)
	if scenario.StepStatus != StepInProgress {
		t.Fatalf("StepStatus = %q", scenario.StepStatus)
	}
	if got := stepStatus(t, scenario, "step_1"); got != StepInProgress {
		t.Fatalf("step_1 = %q", got)
	}
	if got := stepStatus(t, scenario, "step_2"); got != StepNotStarted {
		t.Fatalf("step_2 = %q", got)
	}
	if !strings.Contains(scenario.NextStepHint, "Узнайте правила") {
		t.Fatalf("NextStepHint = %q", scenario.NextStepHint)
	}

	scenario = projectTestScenario(t, fixture, true, nil)
	if got := stepStatus(t, scenario, "step_1"); got != StepDone {
		t.Fatalf("step_1 = %q", got)
	}
	if got := stepStatus(t, scenario, "step_2"); got != StepInProgress {
		t.Fatalf("step_2 = %q", got)
	}
	if !strings.Contains(scenario.NextStepHint, "Заполните заявку") {
		t.Fatalf("NextStepHint = %q", scenario.NextStepHint)
	}
}

func TestProjectScenarioAwaitingConfirmation(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))

	scenario := projectTestScenario(t, fixture, true, &op)
	if scenario.StepStatus != StepInProgress {
		t.Fatalf("StepStatus = %q", scenario.StepStatus)
	}
	if got := stepStatus(t, scenario, "step_2"); got != StepInProgress {
		t.Fatalf("step_2 = %q", got)
	}
	if got := stepStatus(t, scenario, "step_3"); got != StepNotStarted {
		t.Fatalf("step_3 = %q", got)
	}
	if !strings.Contains(scenario.NextStepHint, "Подтвердите карточку") {
		t.Fatalf("NextStepHint = %q", scenario.NextStepHint)
	}
}

func TestProjectScenarioSubmitting(t *testing.T) {
	fixture := newTestFixture(t, "space_auto_backend_wiki")
	op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
	if err := op.BeginSubmission(testUpdatedAt); err != nil {
		t.Fatal(err)
	}

	scenario := projectTestScenario(t, fixture, true, &op)
	if got := stepStatus(t, scenario, "step_2"); got != StepDone {
		t.Fatalf("step_2 = %q", got)
	}
	if got := stepStatus(t, scenario, "step_3"); got != StepInProgress {
		t.Fatalf("step_3 = %q", got)
	}
	if scenario.StepStatus != StepInProgress {
		t.Fatalf("StepStatus = %q", scenario.StepStatus)
	}
	if !strings.Contains(scenario.NextStepHint, "отправляется") {
		t.Fatalf("NextStepHint = %q", scenario.NextStepHint)
	}
}

func TestProjectScenarioFailedAndUnknown(t *testing.T) {
	tests := []struct {
		name       string
		transition func(*Operation)
		hint       string
	}{
		{
			name: "failed",
			transition: func(op *Operation) {
				if err := op.MarkFailed(testUpdatedAt.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			},
			hint: "Можно повторить отправку",
		},
		{
			name: "unknown",
			transition: func(op *Operation) {
				if err := op.MarkUnknown(testUpdatedAt.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			},
			hint: "Результат отправки неизвестен",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newTestFixture(t, "space_auto_backend_wiki")
			op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
			if err := op.BeginSubmission(testUpdatedAt); err != nil {
				t.Fatal(err)
			}
			tt.transition(&op)

			scenario := projectTestScenario(t, fixture, true, &op)
			if got := stepStatus(t, scenario, "step_3"); got != StepBlocked {
				t.Fatalf("step_3 = %q", got)
			}
			if scenario.StepStatus != StepBlocked {
				t.Fatalf("StepStatus = %q", scenario.StepStatus)
			}
			if !strings.Contains(scenario.NextStepHint, tt.hint) {
				t.Fatalf("NextStepHint = %q, want substring %q", scenario.NextStepHint, tt.hint)
			}
		})
	}
}

func TestProjectScenarioTicketStatuses(t *testing.T) {
	tests := []struct {
		status        TicketStatus
		wantStep      StepStatus
		hintContains  string
		hintForbidden string
	}{
		{
			status:        TicketPending,
			wantStep:      StepInProgress,
			hintContains:  "tkt_501",
			hintForbidden: "доступ выдан",
		},
		{
			status:       TicketApproved,
			wantStep:     StepInProgress,
			hintContains: "ещё не выдан",
		},
		{
			status:       TicketRejected,
			wantStep:     StepBlocked,
			hintContains: "отклонена",
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			fixture := newTestFixture(t, "space_auto_backend_wiki")
			op := newAwaitingOperation(t, fixture, defaultPayload(fixture.Resource))
			if err := op.BeginSubmission(testUpdatedAt); err != nil {
				t.Fatal(err)
			}
			ticket := newTestTicket(t, op, tt.status)
			if err := op.MarkSucceeded(ticket, testTicketAt); err != nil {
				t.Fatal(err)
			}

			scenario := projectTestScenario(t, fixture, true, &op)
			if got := stepStatus(t, scenario, "step_3"); got != tt.wantStep {
				t.Fatalf("step_3 = %q, want %q", got, tt.wantStep)
			}
			if !strings.Contains(scenario.NextStepHint, tt.hintContains) {
				t.Fatalf("NextStepHint = %q, want substring %q", scenario.NextStepHint, tt.hintContains)
			}
			if tt.hintForbidden != "" && strings.Contains(scenario.NextStepHint, tt.hintForbidden) {
				t.Fatalf("NextStepHint = %q must not contain %q", scenario.NextStepHint, tt.hintForbidden)
			}
		})
	}
}
