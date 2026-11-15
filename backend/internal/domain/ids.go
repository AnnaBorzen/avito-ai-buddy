package domain

import (
	"fmt"
	"regexp"
	"unicode/utf8"
)

// Typed identifiers prevent accidental mixing of user, team, resource and
// operation IDs at compile time.
type (
	UserID         string
	TeamID         string
	ResourceID     string
	ScenarioID     string
	TaskInstanceID string
	OperationID    string
	TicketID       string
	EventID        string
	IdempotencyKey string
)

// operationIDPattern is fixed by contracts/api.yaml and
// contracts/access-service.openapi.yaml.
var operationIDPattern = regexp.MustCompile(`^op_[0-9]{1,10}$`)

func requireNonEmpty(field, value string) error {
	if value == "" {
		return &ValidationError{Field: field, Message: "must not be empty"}
	}
	return nil
}

func (id UserID) Validate() error {
	return requireNonEmpty("user_id", string(id))
}

func (id TeamID) Validate() error {
	return requireNonEmpty("team_id", string(id))
}

func (id ResourceID) Validate() error {
	return requireNonEmpty("resource_id", string(id))
}

func (id ScenarioID) Validate() error {
	return requireNonEmpty("scenario_id", string(id))
}

func (id TaskInstanceID) Validate() error {
	return requireNonEmpty("task_instance_id", string(id))
}

func (id OperationID) Validate() error {
	if err := requireNonEmpty("operation_id", string(id)); err != nil {
		return err
	}
	if !operationIDPattern.MatchString(string(id)) {
		return &ValidationError{
			Field:   "operation_id",
			Message: fmt.Sprintf("must match %s", operationIDPattern.String()),
		}
	}
	return nil
}

func (id TicketID) Validate() error {
	return requireNonEmpty("ticket_id", string(id))
}

func (id EventID) Validate() error {
	return requireNonEmpty("event_id", string(id))
}

func (id IdempotencyKey) Validate() error {
	n := utf8.RuneCountInString(string(id))
	if n < 8 || n > 128 {
		return &ValidationError{
			Field:   "idempotency_key",
			Message: fmt.Sprintf("length must be between 8 and 128 characters, got %d", n),
		}
	}
	return nil
}
