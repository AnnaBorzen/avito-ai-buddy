package domain

import "testing"

func TestEvaluateAccess(t *testing.T) {
	seed, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed() error = %v", err)
	}

	intern, err := seed.Directory.Profile("usr_intern_01")
	if err != nil {
		t.Fatal(err)
	}
	lead, err := seed.Directory.Profile("usr_lead_01")
	if err != nil {
		t.Fatal(err)
	}

	resource := func(id ResourceID) Resource {
		t.Helper()
		res, err := seed.Catalog.Resource(id)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	tests := []struct {
		name     string
		user     Profile
		resource Resource
		level    AccessLevel
		allowed  bool
		reason   DenyReason
	}{
		{
			name:     "intern own wiki write",
			user:     intern,
			resource: resource("space_auto_backend_wiki"),
			level:    AccessWrite,
			allowed:  true,
		},
		{
			name:     "intern foreign read is allowed",
			user:     intern,
			resource: resource("space_goods_search_wiki"),
			level:    AccessRead,
			allowed:  true,
		},
		{
			name:     "intern foreign write is denied by catalog",
			user:     intern,
			resource: resource("space_goods_search_wiki"),
			level:    AccessWrite,
			allowed:  false,
			reason:   DenyLevelNotInCatalog,
		},
		{
			name:     "intern production read is denied",
			user:     intern,
			resource: resource("db_fintech_prod_cluster"),
			level:    AccessRead,
			allowed:  false,
			reason:   DenyProductionRestricted,
		},
		{
			name:     "intern production admin is denied",
			user:     intern,
			resource: resource("db_fintech_prod_cluster"),
			level:    AccessAdmin,
			allowed:  false,
			reason:   DenyProductionRestricted,
		},
		{
			name:     "lead own resource write",
			user:     lead,
			resource: resource("space_auto_backend_wiki"),
			level:    AccessWrite,
			allowed:  true,
		},
		{
			name:     "lead foreign edit is denied",
			user:     lead,
			resource: resource("space_goods_search_wiki"),
			level:    AccessRead,
			allowed:  false,
			reason:   DenyForbidden,
		},
		{
			name:     "unknown level in catalog is denied",
			user:     intern,
			resource: resource("space_auto_backend_wiki"),
			level:    AccessAdmin,
			allowed:  false,
			reason:   DenyLevelNotInCatalog,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateAccess(tt.user, tt.resource, tt.level)
			if got.Allowed != tt.allowed {
				t.Fatalf("Allowed = %v, want %v", got.Allowed, tt.allowed)
			}
			if !tt.allowed && got.Reason != tt.reason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.reason)
			}
		})
	}
}
