package service

import (
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/ml"
)

// The DTOs below are the JSON shapes of contracts/api.yaml. They intentionally
// contain no internal fields: user/team/approver values always come from the
// server-side directory, never from the request body.

type User struct {
	UserID       string `json:"user_id"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	TeamID       string `json:"team_id"`
	TeamName     string `json:"team_name"`
	MentorHandle string `json:"mentor_handle"`
	ApproverName string `json:"approver_name"`
}

type ScenarioStep struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type Scenario struct {
	ID             string         `json:"scenario_id"`
	Title          string         `json:"title"`
	TaskInstanceID string         `json:"task_instance_id"`
	StepStatus     string         `json:"step_status"`
	Steps          []ScenarioStep `json:"steps"`
	NextStepHint   string         `json:"next_step_hint"`
}

type Source struct {
	DocID       string `json:"doc_id"`
	Title       string `json:"title"`
	IsSynthetic bool   `json:"is_synthetic"`
}

type OperationPayload struct {
	ResourceID    string `json:"resource_id"`
	ResourceTitle string `json:"resource_title"`
	AccessLevel   string `json:"access_level"`
	Justification string `json:"justification"`
	ApproverID    string `json:"approver_id"`
	ApproverName  string `json:"approver_name"`
}

type Ticket struct {
	TicketID       string    `json:"ticket_id"`
	ExternalStatus string    `json:"external_status"`
	CreatedAt      time.Time `json:"created_at"`
	ResourceID     *string   `json:"resource_id,omitempty"`
	AccessLevel    *string   `json:"access_level,omitempty"`
	ApproverName   *string   `json:"approver_name,omitempty"`
}

type Operation struct {
	OperationID     string           `json:"operation_id"`
	OperationStatus string           `json:"operation_status"`
	DraftVersion    int              `json:"draft_version"`
	Payload         OperationPayload `json:"payload"`
	Ticket          *Ticket          `json:"ticket"`
}

type Context struct {
	User            User       `json:"user"`
	Scenario        Scenario   `json:"scenario"`
	ActiveOperation *Operation `json:"active_operation"`
}

type RbacDenial struct {
	ErrorCode   string `json:"error_code"`
	Message     string `json:"message"`
	ResourceID  string `json:"resource_id"`
	AccessLevel string `json:"access_level"`
}

type ChatResponse struct {
	TaskInstanceID    string      `json:"task_instance_id"`
	Reply             string      `json:"reply"`
	Sources           []Source    `json:"sources"`
	ProposedOperation *Operation  `json:"proposed_operation"`
	RbacDenial        *RbacDenial `json:"rbac_denial"`
	HandoffSuggested  bool        `json:"handoff_suggested"`
}

type ConfirmResponse struct {
	OperationID        string  `json:"operation_id"`
	OperationStatus    string  `json:"operation_status"`
	Ticket             *Ticket `json:"ticket"`
	ScenarioStepStatus string  `json:"scenario_step_status"`
	NextStepHint       string  `json:"next_step_hint"`
	// Replay is transport metadata (x-idempotent-replay), not part of the body.
	Replay bool `json:"-"`
}

type ReconcileResponse struct {
	OperationID     string  `json:"operation_id"`
	OperationStatus string  `json:"operation_status"`
	Ticket          *Ticket `json:"ticket"`
	Message         string  `json:"message"`
}

func toUser(view domain.UserView) User {
	return User{
		UserID: string(view.UserID), Name: view.Name, Role: string(view.Role),
		TeamID: string(view.TeamID), TeamName: view.TeamName,
		MentorHandle: view.MentorHandle, ApproverName: view.ApproverName,
	}
}

func toScenario(scenario domain.Scenario) Scenario {
	steps := make([]ScenarioStep, 0, len(scenario.Steps))
	for _, step := range scenario.Steps {
		steps = append(steps, ScenarioStep{
			ID: step.ID, Title: step.Title, Status: string(step.Status),
		})
	}
	return Scenario{
		ID: string(scenario.ID), Title: scenario.Title,
		TaskInstanceID: string(scenario.TaskInstanceID),
		StepStatus:     string(scenario.StepStatus), Steps: steps,
		NextStepHint: scenario.NextStepHint,
	}
}

func toOperation(op *domain.Operation) *Operation {
	if op == nil {
		return nil
	}
	payload := op.Payload
	return &Operation{
		OperationID: string(op.ID), OperationStatus: string(op.Status),
		DraftVersion: op.DraftVersion,
		Payload: OperationPayload{
			ResourceID: string(payload.ResourceID), ResourceTitle: payload.ResourceTitle,
			AccessLevel: string(payload.AccessLevel), Justification: payload.Justification,
			ApproverID: string(payload.ApproverID), ApproverName: payload.ApproverName,
		},
		Ticket: toTicket(op.Ticket),
	}
}

func toTicket(ticket *domain.Ticket) *Ticket {
	if ticket == nil {
		return nil
	}
	out := &Ticket{
		TicketID: string(ticket.ID), ExternalStatus: string(ticket.ExternalStatus),
		CreatedAt: ticket.CreatedAt,
	}
	if ticket.ResourceID != nil {
		value := string(*ticket.ResourceID)
		out.ResourceID = &value
	}
	if ticket.AccessLevel != nil {
		value := string(*ticket.AccessLevel)
		out.AccessLevel = &value
	}
	if ticket.ApproverName != nil {
		value := *ticket.ApproverName
		out.ApproverName = &value
	}
	return out
}

func toSources(sources []ml.Source) []Source {
	out := make([]Source, 0, len(sources))
	for _, source := range sources {
		out = append(out, Source{
			DocID: source.DocID, Title: source.Title, IsSynthetic: source.IsSynthetic,
		})
	}
	return out
}
