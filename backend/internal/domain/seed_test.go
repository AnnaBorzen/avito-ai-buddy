package domain

import (
	"errors"
	"testing"
)

func TestNewSeed(t *testing.T) {
	seed := newTestSeed(t)

	if len(seed.Directory.Profiles()) != 4 {
		t.Fatalf("profiles = %d, want 4", len(seed.Directory.Profiles()))
	}
	if len(seed.Directory.Teams()) != 3 {
		t.Fatalf("teams = %d, want 3", len(seed.Directory.Teams()))
	}
	if len(seed.Catalog.Resources()) != 4 {
		t.Fatalf("resources = %d, want 4", len(seed.Catalog.Resources()))
	}

	intern, err := seed.Directory.UserView("usr_intern_01")
	if err != nil {
		t.Fatal(err)
	}
	if intern.TeamName != "Auto Backend" {
		t.Fatalf("TeamName = %q", intern.TeamName)
	}
	if intern.ApproverName != "Дмитрий Волков" {
		t.Fatalf("ApproverName = %q", intern.ApproverName)
	}
	if intern.MentorHandle != "@ivan_petrov" {
		t.Fatalf("MentorHandle = %q", intern.MentorHandle)
	}

	lead, err := seed.Directory.UserView("usr_lead_01")
	if err != nil {
		t.Fatal(err)
	}
	if lead.ApproverName != "" {
		t.Fatalf("lead ApproverName = %q, want empty", lead.ApproverName)
	}

	if _, err := seed.Directory.ApproverFor("usr_lead_01"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ApproverFor(lead) error = %v, want not found", err)
	}
	if _, err := seed.Directory.Profile("usr_unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Profile(unknown) error = %v, want not found", err)
	}
}

func TestSeedResourceCatalog(t *testing.T) {
	seed := newTestSeed(t)

	prod, err := seed.Catalog.Resource("db_fintech_prod_cluster")
	if err != nil {
		t.Fatal(err)
	}
	if !prod.IsProduction {
		t.Fatal("fintech cluster must be marked as production")
	}
	if prod.Allows(AccessWrite) {
		t.Fatal("fintech cluster must not expose write")
	}
	if !prod.Allows(AccessAdmin) {
		t.Fatal("fintech cluster must expose admin")
	}

	goods, err := seed.Catalog.Resource("space_goods_search_wiki")
	if err != nil {
		t.Fatal(err)
	}
	if goods.Allows(AccessWrite) {
		t.Fatal("goods search wiki must only expose read")
	}

	scenario := seed.Scenario
	if scenario.Version != "1.0" {
		t.Fatalf("scenario version = %q", scenario.Version)
	}
	if len(scenario.Steps) != 3 {
		t.Fatalf("scenario steps = %d, want 3", len(scenario.Steps))
	}
}
