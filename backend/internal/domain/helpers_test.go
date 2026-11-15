package domain

import (
	"testing"
	"time"
)

var (
	testCreatedAt = time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	testUpdatedAt = testCreatedAt.Add(time.Hour)
	testTicketAt  = testCreatedAt.Add(2 * time.Hour)
)

type testFixture struct {
	Seed     Seed
	User     Profile
	Approver Profile
	Resource Resource
}

func newTestSeed(t *testing.T) Seed {
	t.Helper()
	seed, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed() error = %v", err)
	}
	return seed
}

func newTestFixture(t *testing.T, resourceID ResourceID) testFixture {
	t.Helper()
	seed := newTestSeed(t)
	user, err := seed.Directory.Profile("usr_intern_01")
	if err != nil {
		t.Fatal(err)
	}
	approver, err := seed.Directory.ApproverFor(user.UserID)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := seed.Catalog.Resource(resourceID)
	if err != nil {
		t.Fatal(err)
	}
	return testFixture{Seed: seed, User: user, Approver: approver, Resource: resource}
}

func newAwaitingOperation(t *testing.T, fixture testFixture, input PayloadInput) Operation {
	t.Helper()
	op, err := NewAwaitingConfirmation(
		"op_1",
		fixture.User,
		"tsk_901",
		fixture.Seed.Scenario.ID,
		input,
		fixture.Resource,
		fixture.Approver,
		testCreatedAt,
	)
	if err != nil {
		t.Fatalf("NewAwaitingConfirmation() error = %v", err)
	}
	return op
}

func defaultPayload(resource Resource) PayloadInput {
	level := AccessRead
	for _, allowed := range resource.Levels {
		if allowed.Requestable() {
			level = allowed
			break
		}
	}
	return PayloadInput{
		ResourceID:    resource.ID,
		AccessLevel:   level,
		Justification: "Прохождение онбординга первой недели",
	}
}

func newTestTicket(t *testing.T, op Operation, status TicketStatus) Ticket {
	t.Helper()
	resourceID := op.Payload.ResourceID
	level := op.Payload.AccessLevel
	ticket, err := NewTicket(
		"tkt_501",
		op.ID,
		status,
		&resourceID,
		&level,
		ptr("Дмитрий Волков"),
		testTicketAt,
	)
	if err != nil {
		t.Fatalf("NewTicket() error = %v", err)
	}
	return ticket
}

func ptr[T any](value T) *T {
	return &value
}
