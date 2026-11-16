// Package access is the HTTP client of the access-request service. The
// contract is contracts/access-service.openapi.yaml.
package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrTimeout means no response arrived in time. The request may have been
	// processed, so the backend must move the operation to "unknown".
	ErrTimeout = errors.New("access service timeout")
	// ErrUnavailable means the service did not accept/find the request and a
	// retry is safe.
	ErrUnavailable = errors.New("access service unavailable")
	// ErrRejected means the service rejected the request payload.
	ErrRejected = errors.New("access service rejected request")
)

// CreateRequest is the trusted payload built by the backend.
type CreateRequest struct {
	OperationID   string `json:"operation_id"`
	UserID        string `json:"user_id"`
	TeamID        string `json:"team_id"`
	ResourceID    string `json:"resource_id"`
	AccessLevel   string `json:"access_level"`
	Justification string `json:"justification"`
	ApproverID    string `json:"approver_id"`
	ApproverName  string `json:"approver_name"`
}

// Ticket mirrors TicketRecord from the access-service contract.
type Ticket struct {
	TicketID       string    `json:"ticket_id"`
	OperationID    string    `json:"operation_id"`
	ExternalStatus string    `json:"external_status"`
	CreatedAt      time.Time `json:"created_at"`
	ResourceID     *string   `json:"resource_id,omitempty"`
	AccessLevel    *string   `json:"access_level,omitempty"`
	ApproverName   *string   `json:"approver_name,omitempty"`
}

type lookupResponse struct {
	Found  bool    `json:"found"`
	Ticket *Ticket `json:"ticket"`
}

// Client is an HTTP client of the access service.
type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}}
}

// Create submits a ticket. A repeated operation_id returns the existing ticket.
func (c *Client) Create(ctx context.Context, idempotencyKey string, req CreateRequest) (*Ticket, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("access: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/internal/access-requests", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("access: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Idempotency-Key", idempotencyKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, classifyTransport(err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrUnavailable, readErr)
	}

	switch {
	case resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK:
		var ticket Ticket
		if err := json.Unmarshal(raw, &ticket); err != nil {
			return nil, fmt.Errorf("%w: decode ticket: %v", ErrUnavailable, err)
		}
		return &ticket, nil
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("%w: status %d: %s", ErrRejected, resp.StatusCode, strings.TrimSpace(string(raw)))
	default:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrUnavailable, resp.StatusCode)
	}
}

// Lookup checks whether a ticket exists without changing state.
func (c *Client) Lookup(ctx context.Context, operationID string) (*Ticket, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/internal/access-requests?operation_id="+operationID, nil)
	if err != nil {
		return nil, false, fmt.Errorf("access: build lookup: %w", err)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, false, classifyTransport(err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, false, fmt.Errorf("%w: read body: %v", ErrUnavailable, readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("%w: status %d: %s", ErrUnavailable, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out lookupResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, fmt.Errorf("%w: decode lookup: %v", ErrUnavailable, err)
	}
	if !out.Found {
		return nil, false, nil
	}
	if out.Ticket == nil {
		return nil, false, fmt.Errorf("%w: found lookup without ticket", ErrUnavailable)
	}
	return out.Ticket, true, nil
}

func (c *Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/readyz", nil)
	if err != nil {
		return fmt.Errorf("access: build readiness request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}

func classifyTransport(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
