package domain

import "fmt"

// Seed is the validated synthetic reference data from ТЗ §7/§11.
type Seed struct {
	Directory Directory
	Catalog   Catalog
	Scenario  ScenarioDefinition
}

func NewSeed() (Seed, error) {
	directory, err := NewDirectory(SeedProfiles(), SeedTeams())
	if err != nil {
		return Seed{}, fmt.Errorf("seed directory: %w", err)
	}
	catalog, err := NewCatalog(SeedResources())
	if err != nil {
		return Seed{}, fmt.Errorf("seed catalog: %w", err)
	}
	scenario := SeedScenario()
	if err := scenario.Validate(); err != nil {
		return Seed{}, fmt.Errorf("seed scenario: %w", err)
	}
	return Seed{Directory: directory, Catalog: catalog, Scenario: scenario}, nil
}

func SeedTeams() []Team {
	return []Team{
		{ID: "team_auto_backend", Name: "Auto Backend", LeadID: "usr_lead_01"},
		{ID: "team_goods_search", Name: "Goods Search", LeadID: "usr_lead_02"},
		{ID: "team_fintech_core", Name: "Fintech Core"},
	}
}

func SeedProfiles() []Profile {
	return []Profile{
		{
			UserID:       "usr_intern_01",
			Name:         "Алексей Смирнов",
			Role:         RoleIntern,
			TeamID:       "team_auto_backend",
			MentorHandle: "@ivan_petrov",
		},
		{
			UserID:       "usr_intern_02",
			Name:         "Мария Иванова",
			Role:         RoleIntern,
			TeamID:       "team_goods_search",
			MentorHandle: "@olga_sidorova",
		},
		{
			UserID: "usr_lead_01",
			Name:   "Дмитрий Волков",
			Role:   RoleLead,
			TeamID: "team_auto_backend",
		},
		{
			UserID: "usr_lead_02",
			Name:   "Тимлид Goods Search",
			Role:   RoleLead,
			TeamID: "team_goods_search",
		},
	}
}

func SeedResources() []Resource {
	return []Resource{
		{
			ID:          "space_auto_backend_wiki",
			Title:       "Пространство базы знаний Auto Backend",
			OwnerTeamID: "team_auto_backend",
			Levels:      []AccessLevel{AccessRead, AccessWrite},
		},
		{
			ID:          "repo_auto_core_sandbox",
			Title:       "Учебный Git-репозиторий песочницы Auto Core",
			OwnerTeamID: "team_auto_backend",
			Levels:      []AccessLevel{AccessRead, AccessWrite},
		},
		{
			ID:          "space_goods_search_wiki",
			Title:       "Пространство базы знаний Поиска Товаров",
			OwnerTeamID: "team_goods_search",
			Levels:      []AccessLevel{AccessRead},
		},
		{
			ID:           "db_fintech_prod_cluster",
			Title:        "Боевой кластер БД финансовых сервисов (Prod)",
			OwnerTeamID:  "team_fintech_core",
			Levels:       []AccessLevel{AccessRead, AccessAdmin},
			IsProduction: true,
		},
	}
}

func SeedScenario() ScenarioDefinition {
	return ScenarioDefinition{
		ID:      "scn_team_space_access",
		Title:   "Получение доступа в пространство команды",
		Version: "1.0",
		Steps: []StepDefinition{
			{ID: StepRulesID, Title: "Узнать правила запроса доступа"},
			{ID: StepRequestID, Title: "Сформировать и подтвердить заявку"},
			{ID: StepTicketID, Title: "Ожидание согласования тимлидом"},
		},
	}
}
