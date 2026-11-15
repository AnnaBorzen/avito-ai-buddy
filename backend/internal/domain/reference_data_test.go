package domain

import (
	"errors"
	"testing"
)

func TestResourceValidation(t *testing.T) {
	valid := Resource{
		ID:          "space_auto_backend_wiki",
		Title:       "Пространство базы знаний Auto Backend",
		OwnerTeamID: "team_auto_backend",
		Levels:      []AccessLevel{AccessRead, AccessWrite},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Resource.Validate() error = %v", err)
	}
	if !valid.Allows(AccessWrite) || valid.Allows(AccessAdmin) {
		t.Fatal("Allows() must follow the resource catalog")
	}

	tests := []struct {
		name   string
		mutate func(*Resource)
	}{
		{name: "empty id", mutate: func(r *Resource) { r.ID = "" }},
		{name: "empty title", mutate: func(r *Resource) { r.Title = "" }},
		{name: "empty owner", mutate: func(r *Resource) { r.OwnerTeamID = "" }},
		{name: "empty levels", mutate: func(r *Resource) { r.Levels = nil }},
		{name: "unknown level", mutate: func(r *Resource) { r.Levels = []AccessLevel{"owner"} }},
		{name: "duplicate level", mutate: func(r *Resource) { r.Levels = []AccessLevel{AccessRead, AccessRead} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := valid
			tt.mutate(&resource)
			if err := resource.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestCatalogValidation(t *testing.T) {
	resources := SeedResources()
	catalog, err := NewCatalog(resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Resources()) != len(resources) {
		t.Fatalf("Resources() = %d, want %d", len(catalog.Resources()), len(resources))
	}
	if _, err := catalog.Resource("space_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resource(missing) error = %v, want not found", err)
	}

	duplicate := append(append([]Resource(nil), resources...), resources[0])
	if _, err := NewCatalog(duplicate); !errors.Is(err, ErrValidation) {
		t.Fatalf("NewCatalog(duplicate) error = %v, want validation error", err)
	}
	if _, err := NewCatalog([]Resource{{ID: "broken"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("NewCatalog(invalid) error = %v, want validation error", err)
	}
}

func TestDirectoryValidation(t *testing.T) {
	teams := SeedTeams()
	profiles := SeedProfiles()

	directory, err := NewDirectory(profiles, teams)
	if err != nil {
		t.Fatal(err)
	}
	if name, err := directory.TeamName("team_auto_backend"); err != nil || name != "Auto Backend" {
		t.Fatalf("TeamName() = %q, %v", name, err)
	}
	if _, err := directory.Team("team_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Team(missing) error = %v", err)
	}
	if _, err := directory.UserView("usr_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UserView(missing) error = %v", err)
	}
	if len(directory.Profiles()) != len(profiles) || len(directory.Teams()) != len(teams) {
		t.Fatal("directory projections must preserve seed order and size")
	}

	duplicateTeam := append(append([]Team(nil), teams...), teams[0])
	if _, err := NewDirectory(profiles, duplicateTeam); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate team error = %v, want validation", err)
	}

	duplicateProfile := append(append([]Profile(nil), profiles...), profiles[0])
	if _, err := NewDirectory(duplicateProfile, teams); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate profile error = %v, want validation", err)
	}

	unknownTeam := append([]Profile(nil), profiles...)
	unknownTeam[0].TeamID = "team_missing"
	if _, err := NewDirectory(unknownTeam, teams); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown team error = %v, want not found", err)
	}

	missingLead := append([]Team(nil), teams...)
	missingLead[0].LeadID = "usr_missing"
	if _, err := NewDirectory(profiles, missingLead); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing lead error = %v, want not found", err)
	}

	invalidProfile := append([]Profile(nil), profiles...)
	invalidProfile[0].Name = ""
	if _, err := NewDirectory(invalidProfile, teams); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid profile error = %v, want validation", err)
	}
}

func TestScenarioDefinitionValidation(t *testing.T) {
	valid := SeedScenario()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid scenario error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ScenarioDefinition)
	}{
		{name: "empty id", mutate: func(s *ScenarioDefinition) { s.ID = "" }},
		{name: "empty title", mutate: func(s *ScenarioDefinition) { s.Title = "" }},
		{name: "empty version", mutate: func(s *ScenarioDefinition) { s.Version = "" }},
		{name: "empty steps", mutate: func(s *ScenarioDefinition) { s.Steps = nil }},
		{name: "empty step id", mutate: func(s *ScenarioDefinition) { s.Steps[0].ID = "" }},
		{name: "empty step title", mutate: func(s *ScenarioDefinition) { s.Steps[0].Title = "" }},
		{name: "duplicate step", mutate: func(s *ScenarioDefinition) { s.Steps[1].ID = s.Steps[0].ID }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenario := SeedScenario()
			tt.mutate(&scenario)
			if err := scenario.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate() error = %v, want validation error", err)
			}
		})
	}
}
