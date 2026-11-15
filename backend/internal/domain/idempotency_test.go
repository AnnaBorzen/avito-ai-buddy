package domain

import (
	"errors"
	"testing"
	"time"
)

func completedIdempotencyRecord() IdempotencyRecord {
	return IdempotencyRecord{
		UserID:         "usr_intern_01",
		OperationID:    "op_1",
		Key:            "idem-key-0001",
		RequestHash:    "sha256:abc123",
		ResponseStatus: ptr(200),
		ResponseBody:   []byte(`{"operation_status":"succeeded"}`),
		CreatedAt:      testCreatedAt,
	}
}

func TestIdempotencyRecordValidate(t *testing.T) {
	if err := completedIdempotencyRecord().Validate(); err != nil {
		t.Fatalf("completed record Validate() error = %v", err)
	}

	pending := completedIdempotencyRecord()
	pending.ResponseStatus = nil
	pending.ResponseBody = nil
	if err := pending.Validate(); err != nil {
		t.Fatalf("pending record Validate() error = %v", err)
	}
	if pending.Completed() {
		t.Fatal("pending record must not be completed")
	}

	tests := []struct {
		name   string
		mutate func(*IdempotencyRecord)
	}{
		{
			name: "short key",
			mutate: func(r *IdempotencyRecord) {
				r.Key = "short"
			},
		},
		{
			name: "missing user",
			mutate: func(r *IdempotencyRecord) {
				r.UserID = ""
			},
		},
		{
			name: "body without status",
			mutate: func(r *IdempotencyRecord) {
				r.ResponseStatus = nil
			},
		},
		{
			name: "status without body",
			mutate: func(r *IdempotencyRecord) {
				r.ResponseBody = nil
			},
		},
		{
			name: "empty completed body",
			mutate: func(r *IdempotencyRecord) {
				r.ResponseBody = []byte{}
			},
		},
		{
			name: "invalid status",
			mutate: func(r *IdempotencyRecord) {
				r.ResponseStatus = ptr(99)
			},
		},
		{
			name: "missing created at",
			mutate: func(r *IdempotencyRecord) {
				r.CreatedAt = time.Time{}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := completedIdempotencyRecord()
			tt.mutate(&record)
			if err := record.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestDecideIdempotency(t *testing.T) {
	record := completedIdempotencyRecord()

	tests := []struct {
		name   string
		record *IdempotencyRecord
		hash   string
		want   IdempotencyDecision
	}{
		{name: "new", record: nil, hash: "sha256:abc123", want: IdempotencyNew},
		{name: "replay", record: &record, hash: "sha256:abc123", want: IdempotencyReplay},
		{name: "conflict", record: &record, hash: "sha256:other", want: IdempotencyConflict},
		{
			name: "in progress",
			record: func() *IdempotencyRecord {
				pending := record
				pending.ResponseStatus = nil
				pending.ResponseBody = nil
				return &pending
			}(),
			hash: "sha256:abc123",
			want: IdempotencyInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideIdempotency(tt.record, tt.hash); got != tt.want {
				t.Fatalf("DecideIdempotency() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdempotencyRecordExpired(t *testing.T) {
	record := completedIdempotencyRecord()
	if record.Expired(testCreatedAt.Add(23 * time.Hour)) {
		t.Fatal("record must not expire before TTL")
	}
	if !record.Expired(testCreatedAt.Add(IdempotencyTTL)) {
		t.Fatal("record must expire at TTL")
	}
}
