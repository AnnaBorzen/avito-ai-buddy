package domain

import "fmt"

// StepStatus is a UI-facing status of one scenario step.
type StepStatus string

const (
	StepNotStarted StepStatus = "not_started"
	StepInProgress StepStatus = "in_progress"
	StepBlocked    StepStatus = "blocked"
	StepDone       StepStatus = "done"
)

// StepDefinition is the static part of a scenario step.
type StepDefinition struct {
	ID    string
	Title string
}

// Step IDs are the contract between the scenario definition and the state
// projection; titles and order stay data-driven.
const (
	StepRulesID   = "step_1"
	StepRequestID = "step_2"
	StepTicketID  = "step_3"
)

// ScenarioDefinition is reference data that owns step order and titles.
type ScenarioDefinition struct {
	ID      ScenarioID
	Title   string
	Version string
	Steps   []StepDefinition
}

func (d ScenarioDefinition) Validate() error {
	if err := d.ID.Validate(); err != nil {
		return err
	}
	if err := requireNonEmpty("scenario.title", d.Title); err != nil {
		return err
	}
	if err := requireNonEmpty("scenario.version", d.Version); err != nil {
		return err
	}
	if len(d.Steps) == 0 {
		return &ValidationError{Field: "scenario.steps", Message: "must not be empty"}
	}
	seen := make(map[string]struct{}, len(d.Steps))
	for _, step := range d.Steps {
		if err := requireNonEmpty("scenario.step.id", step.ID); err != nil {
			return err
		}
		if err := requireNonEmpty("scenario.step.title", step.Title); err != nil {
			return err
		}
		if _, exists := seen[step.ID]; exists {
			return &ValidationError{Field: "scenario.steps", Message: fmt.Sprintf("duplicate step %q", step.ID)}
		}
		seen[step.ID] = struct{}{}
	}
	return nil
}

// ScenarioInput is everything the projection needs from the service. Chat and
// task progress are not persisted by the domain.
type ScenarioInput struct {
	TaskInstanceID TaskInstanceID
	RulesExplained bool
	Operation      *Operation
}

// Step is one projected scenario step.
type Step struct {
	ID     string
	Title  string
	Status StepStatus
}

// Scenario is the projected scenario state returned by GET /context.
type Scenario struct {
	ID             ScenarioID
	Title          string
	TaskInstanceID TaskInstanceID
	StepStatus     StepStatus
	Steps          []Step
	NextStepHint   string
}

// ProjectScenario derives step statuses and the current hint from the operation
// state. The operation's ticket is the source of truth for approval progress.
func ProjectScenario(def ScenarioDefinition, input ScenarioInput) (Scenario, error) {
	if err := def.Validate(); err != nil {
		return Scenario{}, err
	}
	if err := input.TaskInstanceID.Validate(); err != nil {
		return Scenario{}, err
	}
	if input.Operation != nil {
		if err := input.Operation.Validate(); err != nil {
			return Scenario{}, err
		}
	}

	// An existing operation proves that the user already reached the proposal
	// step, even if the caller did not pass RulesExplained explicitly.
	step1Done := input.RulesExplained || input.Operation != nil
	step2Done := operationConfirmed(input.Operation)
	step3Status := thirdStepStatus(input.Operation, step2Done)

	step1Status := StepInProgress
	if step1Done {
		step1Status = StepDone
	}
	step2Status := StepNotStarted
	if step2Done {
		step2Status = StepDone
	} else if step1Done {
		step2Status = StepInProgress
	}

	statusByStepID := map[string]StepStatus{
		StepRulesID:   step1Status,
		StepRequestID: step2Status,
		StepTicketID:  step3Status,
	}
	steps := make([]Step, 0, len(def.Steps))
	for _, step := range def.Steps {
		status := StepNotStarted
		if known, ok := statusByStepID[step.ID]; ok {
			status = known
		}
		steps = append(steps, Step{ID: step.ID, Title: step.Title, Status: status})
	}

	current := step1Status
	if step1Done {
		current = step2Status
	}
	if step2Done {
		current = step3Status
	}

	return Scenario{
		ID:             def.ID,
		Title:          def.Title,
		TaskInstanceID: input.TaskInstanceID,
		StepStatus:     current,
		Steps:          steps,
		NextStepHint:   nextStepHint(input.RulesExplained, input.Operation),
	}, nil
}

func operationConfirmed(op *Operation) bool {
	if op == nil {
		return false
	}
	switch op.Status {
	case StatusSubmitting, StatusSucceeded, StatusFailed, StatusUnknown:
		return true
	default:
		return false
	}
}

func thirdStepStatus(op *Operation, step2Done bool) StepStatus {
	if !step2Done || op == nil {
		return StepNotStarted
	}
	if op.Ticket != nil {
		switch op.Ticket.ExternalStatus {
		case TicketPending, TicketApproved:
			return StepInProgress
		case TicketRejected:
			return StepBlocked
		}
	}
	if op.Status == StatusSubmitting {
		return StepInProgress
	}
	return StepBlocked
}

func nextStepHint(rulesExplained bool, op *Operation) string {
	if op == nil {
		if rulesExplained {
			return "Заполните заявку на доступ и подтвердите отправку."
		}
		return "Узнайте правила запроса доступа или уточните детали у наставника."
	}

	if op.Ticket != nil {
		switch op.Ticket.ExternalStatus {
		case TicketPending:
			return fmt.Sprintf(
				"Заявка %s создана и ожидает согласования. Доступ пока не активирован.",
				op.Ticket.ID,
			)
		case TicketApproved:
			return "Заявка согласована, но доступ ещё не выдан. Дождитесь технической выдачи прав."
		case TicketRejected:
			return "Заявка отклонена. Уточните причину отказа у руководителя или наставника."
		}
	}

	switch op.Status {
	case StatusAwaitingConfirmation, StatusDraft:
		return "Подтвердите карточку запроса или уточните параметры в чате."
	case StatusSubmitting:
		return "Заявка отправляется. Дождитесь ответа сервиса."
	case StatusUnknown:
		return "Результат отправки неизвестен. Проверьте статус операции перед повторной отправкой."
	case StatusFailed:
		return "Сервис подтвердил, что заявки нет. Можно повторить отправку."
	}
	return "Проверьте состояние заявки."
}
