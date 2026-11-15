package domain

import "time"

// IdempotencyTTL is the replay window from specs/access-request.md.
const IdempotencyTTL = 24 * time.Hour

// IdempotencyRecord is scoped by (user_id, operation_id, key). A nil
// ResponseStatus means the key is reserved while the external call is in
// flight; a completed record has both status and body.
type IdempotencyRecord struct {
	UserID         UserID
	OperationID    OperationID
	Key            IdempotencyKey
	RequestHash    string
	ResponseStatus *int
	ResponseBody   []byte
	CreatedAt      time.Time
}

func (r IdempotencyRecord) Validate() error {
	if err := r.UserID.Validate(); err != nil {
		return err
	}
	if err := r.OperationID.Validate(); err != nil {
		return err
	}
	if err := r.Key.Validate(); err != nil {
		return err
	}
	if err := requireNonEmpty("request_hash", r.RequestHash); err != nil {
		return err
	}
	if r.CreatedAt.IsZero() {
		return &ValidationError{Field: "created_at", Message: "must be set"}
	}

	switch {
	case r.ResponseStatus == nil && r.ResponseBody != nil:
		return &ValidationError{Field: "response_body", Message: "must be nil while the key is pending"}
	case r.ResponseStatus != nil && r.ResponseBody == nil:
		return &ValidationError{Field: "response_body", Message: "must be set for a completed key"}
	case r.ResponseStatus != nil && *r.ResponseStatus < 100 || r.ResponseStatus != nil && *r.ResponseStatus > 599:
		return &ValidationError{Field: "response_status", Message: "must be a valid HTTP status"}
	case r.ResponseBody != nil && len(r.ResponseBody) == 0:
		return &ValidationError{Field: "response_body", Message: "must not be empty"}
	}
	return nil
}

func (r IdempotencyRecord) Completed() bool {
	return r.ResponseStatus != nil && r.ResponseBody != nil
}

func (r IdempotencyRecord) Expired(now time.Time) bool {
	return !r.CreatedAt.IsZero() && now.Sub(r.CreatedAt) >= IdempotencyTTL
}

// IdempotencyDecision tells the service whether it may call the upstream
// service, replay a stored response or must reject a changed request.
type IdempotencyDecision int

const (
	IdempotencyNew IdempotencyDecision = iota
	IdempotencyReplay
	IdempotencyInProgress
	IdempotencyConflict
)

func DecideIdempotency(existing *IdempotencyRecord, requestHash string) IdempotencyDecision {
	if existing == nil {
		return IdempotencyNew
	}
	if existing.RequestHash != requestHash {
		return IdempotencyConflict
	}
	if !existing.Completed() {
		return IdempotencyInProgress
	}
	return IdempotencyReplay
}
