// Package ml — клиент ML-сервиса агента (ml/, контракт contracts/ml-api.yaml).
//
// ML-сервис stateless: backend передаёт доверенный контекст пользователя из сессии,
// права на ресурсы, текущий черновик и последнюю операцию, а получает ответ агента,
// источники и (опционально) tool_call propose_access_request. Backend обязан сам
// провалидировать аргументы tool_call и проверить RBAC перед созданием черновика (FR-05, FR-09).
package ml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrUnavailable — сеть, таймаут или 5xx ML-сервиса. Обработчик чата должен вернуть 502
// и предложить повторить запрос, не меняя состояние операции.
var ErrUnavailable = errors.New("ml service unavailable")

// ErrBadRequest — ML-сервис отклонил запрос (400): ошибка формирования запроса на backend.
var ErrBadRequest = errors.New("ml service rejected request")

type UserContext struct {
	UserID       string `json:"user_id"`
	Name         string `json:"name,omitempty"`
	Role         string `json:"role"`
	TeamID       string `json:"team_id"`
	TeamName     string `json:"team_name,omitempty"`
	MentorHandle string `json:"mentor_handle,omitempty"`
	ApproverName string `json:"approver_name,omitempty"`
}

type Permission struct {
	ResourceID    string   `json:"resource_id"`
	AllowedLevels []string `json:"allowed_levels"`
}

type Slots struct {
	ResourceID    *string `json:"resource_id"`
	AccessLevel   *string `json:"access_level"`
	Justification *string `json:"justification"`
}

type Ticket struct {
	TicketID       string `json:"ticket_id,omitempty"`
	ExternalStatus string `json:"external_status,omitempty"`
	ResourceID     string `json:"resource_id,omitempty"`
	AccessLevel    string `json:"access_level,omitempty"`
}

type Operation struct {
	OperationID     string  `json:"operation_id,omitempty"`
	OperationStatus string  `json:"operation_status"`
	Ticket          *Ticket `json:"ticket"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type TurnRequest struct {
	RequestID      string       `json:"request_id,omitempty"`
	TaskInstanceID string       `json:"task_instance_id,omitempty"`
	User           UserContext  `json:"user"`
	Permissions    []Permission `json:"permissions,omitempty"`
	Draft          *Slots       `json:"draft"`
	ExpectedSlot   *string      `json:"expected_slot"`
	Operation      *Operation   `json:"operation"`
	History        []Message    `json:"history,omitempty"`
	Message        string       `json:"message"`
}

type Source struct {
	DocID       string  `json:"doc_id"`
	Title       string  `json:"title"`
	Section     string  `json:"section"`
	Excerpt     string  `json:"excerpt"`
	IsSynthetic bool    `json:"is_synthetic"`
	SourceType  string  `json:"source_type"`
	SourceLabel string  `json:"source_label"`
	SourceURL   *string `json:"source_url"`
	Score       float64 `json:"score"`
}

type ToolCall struct {
	Name      string `json:"name"`
	Arguments struct {
		ResourceID    string `json:"resource_id"`
		AccessLevel   string `json:"access_level"`
		Justification string `json:"justification"`
	} `json:"arguments"`
}

type Policy struct {
	Allowed       bool     `json:"allowed"`
	Code          string   `json:"code"`
	Message       string   `json:"message"`
	AllowedLevels []string `json:"allowed_levels"`
}

type Guard struct {
	InjectionDetected bool     `json:"injection_detected"`
	Flags             []string `json:"flags"`
}

type Meta struct {
	Mode            string  `json:"mode"`
	Model           string  `json:"model"`
	Retriever       string  `json:"retriever"`
	LatencyMS       float64 `json:"latency_ms"`
	ScenarioID      string  `json:"scenario_id"`
	ScenarioVersion string  `json:"scenario_version"`
	ServiceVersion  string  `json:"service_version"`
	LLMError        string  `json:"llm_error,omitempty"`
}

type Analytics struct {
	Event      string         `json:"event"`
	Properties map[string]any `json:"properties"`
}

type TurnResponse struct {
	Reply            string    `json:"reply"`
	ReplyKind        string    `json:"reply_kind"`
	Intent           string    `json:"intent"`
	IntentConfidence float64   `json:"intent_confidence"`
	Sources          []Source  `json:"sources"`
	Slots            Slots     `json:"slots"`
	MissingSlots     []string  `json:"missing_slots"`
	ExpectedSlot     *string   `json:"expected_slot"`
	ToolCall         *ToolCall `json:"tool_call"`
	Policy           *Policy   `json:"policy"`
	HandoffSuggested bool      `json:"handoff_suggested"`
	HandoffTarget    *string   `json:"handoff_target"`
	Guard            Guard     `json:"guard"`
	Meta             Meta      `json:"meta"`
	Analytics        Analytics `json:"analytics"`
}

// Client — HTTP-клиент ML-сервиса. Безопасен для конкурентного использования.
type Client struct {
	baseURL string
	http    *http.Client
}

// New создаёт клиент. timeout должен быть больше LLM_TIMEOUT ML-сервиса,
// иначе backend оборвёт запрос раньше, чем ML успеет откатиться на локальный режим.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}}
}

// Turn выполняет один ход диалога.
func (c *Client) Turn(ctx context.Context, req TurnRequest) (*TurnResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ml: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/agent/turn", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ml: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.RequestID != "" {
		httpReq.Header.Set("X-Request-Id", req.RequestID)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrUnavailable, err)
	}
	switch {
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("%w: status %d: %s", ErrBadRequest, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out TurnResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}
	return &out, nil
}

// Ready проверяет /readyz ML-сервиса (для /readyz backend).
func (c *Client) Ready(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/readyz", nil)
	if err != nil {
		return fmt.Errorf("ml: build request: %w", err)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}
