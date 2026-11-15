package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestDomainErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   string
	}{
		{
			name:   "validation",
			err:    &ValidationError{Field: "resource_id", Message: "must not be empty"},
			target: ErrValidation,
			want:   "domain: invalid resource_id: must not be empty",
		},
		{
			name:   "not found with id",
			err:    &NotFoundError{Entity: "resource", ID: "space_missing"},
			target: ErrNotFound,
			want:   `domain: resource "space_missing" not found`,
		},
		{
			name:   "not found without id",
			err:    &NotFoundError{Entity: "approver"},
			target: ErrNotFound,
			want:   "domain: approver not found",
		},
		{
			name:   "conflict",
			err:    &ConflictError{Message: "cannot submit"},
			target: ErrConflict,
			want:   "domain: conflict: cannot submit",
		},
		{
			name:   "draft version conflict",
			err:    &DraftVersionConflictError{Expected: 1, Actual: 2},
			target: ErrConflict,
			want:   "domain: draft version conflict: expected 1, actual 2",
		},
		{
			name: "access denied",
			err: &AccessDeniedError{
				UserID:      "usr_intern_01",
				Role:        RoleIntern,
				ResourceID:  "space_goods_search_wiki",
				AccessLevel: AccessWrite,
				Reason:      DenyLevelNotInCatalog,
			},
			target: ErrForbidden,
			want:   `domain: access denied for user "usr_intern_01" to resource "space_goods_search_wiki" level "write"`,
		},
		{
			name:   "invariant",
			err:    &InvariantError{Message: "ticket belongs to another operation"},
			target: ErrInvariant,
			want:   "domain: invariant violated: ticket belongs to another operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.target) {
				t.Fatalf("errors.Is(%v, %v) = false", tt.err, tt.target)
			}
			if message := tt.err.Error(); !strings.Contains(message, tt.want) {
				t.Fatalf("Error() = %q, want substring %q", message, tt.want)
			}
		})
	}
}
