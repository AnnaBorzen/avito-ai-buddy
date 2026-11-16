package accessservice_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/accessservice"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/access"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/testdb"
)

func TestServerOwnsTicketsAndReplaysByOperationID(t *testing.T) {
	st := testdb.Spawn(t)
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	server := accessservice.New(addr, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("access service stop: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("access service did not stop")
		}
	})
	waitReady(t, addr)

	client := access.New("http://"+addr, 5*time.Second)
	request := access.CreateRequest{
		OperationID: "op_1", UserID: "usr_intern_01", TeamID: "team_auto_backend",
		ResourceID: "space_auto_backend_wiki", AccessLevel: "read",
		Justification: "Нужен доступ к базе знаний команды для онбординга.",
		ApproverID:    "usr_lead_01", ApproverName: "Дмитрий Волков",
	}

	first, err := client.Create(ctx, "idem-key-0001", request)
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if first.TicketID == "" || first.OperationID != "op_1" || first.ExternalStatus != "pending" {
		t.Fatalf("unexpected ticket: %+v", first)
	}

	// Repeating the same operation is idempotent and returns the stored ticket.
	replay, err := client.Create(ctx, "idem-key-0002", request)
	if err != nil {
		t.Fatalf("replay ticket: %v", err)
	}
	if replay.TicketID != first.TicketID {
		t.Fatalf("replay ticket_id = %q, want %q", replay.TicketID, first.TicketID)
	}

	found, ok, err := client.Lookup(ctx, "op_1")
	if err != nil || !ok || found == nil {
		t.Fatalf("lookup op_1 = %+v, %v, %v", found, ok, err)
	}
	if found.TicketID != first.TicketID {
		t.Fatalf("lookup ticket_id = %q, want %q", found.TicketID, first.TicketID)
	}

	missing, ok, err := client.Lookup(ctx, "op_2")
	if err != nil || ok || missing != nil {
		t.Fatalf("lookup missing = %+v, %v, %v", missing, ok, err)
	}

	// The same operation_id with different data is a conflict, not a silent
	// overwrite: the service owns the ticket once it is created.
	conflicting := request
	conflicting.AccessLevel = "write"
	if _, err := client.Create(ctx, "idem-key-0003", conflicting); err == nil {
		t.Fatal("create with conflicting payload succeeded, want rejection")
	}

	invalid := request
	invalid.OperationID = "op_3"
	invalid.AccessLevel = "admin"
	if _, err := client.Create(ctx, "idem-key-0004", invalid); err == nil {
		t.Fatal("create with unsupported access_level succeeded, want rejection")
	}
}

func TestSimulateModeFailsBeforeWritingTicket(t *testing.T) {
	st := testdb.Spawn(t)
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	server := accessservice.New(addr, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitReady(t, addr)

	payload := `{"operation_id":"op_9","user_id":"usr_intern_01","team_id":"team_auto_backend",` +
		`"resource_id":"space_auto_backend_wiki","access_level":"read",` +
		`"justification":"Нужен доступ к базе знаний команды.","approver_id":"usr_lead_01",` +
		`"approver_name":"Дмитрий Волков"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+addr+"/internal/access-requests", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-key-sim-01")
	req.Header.Set("X-Simulate-Mode", "service_error")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("simulated call: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}

	client := access.New("http://"+addr, 5*time.Second)
	if ticket, found, err := client.Lookup(ctx, "op_9"); err != nil || found || ticket != nil {
		t.Fatalf("simulated failure wrote a ticket: %+v, %v, %v", ticket, found, err)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func waitReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("access service at %s did not become ready", addr)
}
