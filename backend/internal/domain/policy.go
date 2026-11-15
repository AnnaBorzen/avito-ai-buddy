package domain

// DenyReason identifies the exact policy step that rejected a request.
type DenyReason string

const (
	DenyLevelNotInCatalog    DenyReason = "level_not_in_catalog"
	DenyProductionRestricted DenyReason = "production_restricted"
	DenyForeignTeamWrite     DenyReason = "foreign_team_write"
	DenyForbidden            DenyReason = "forbidden"
)

// Decision is the single RBAC policy result for propose, preview and confirm.
type Decision struct {
	Allowed bool
	Reason  DenyReason
}

// EvaluateAccess implements specs/access-request.md §11.3. It receives an
// already resolved resource from the catalog, so production status and allowed
// levels are explicit data, never inferred from IDs or titles.
func EvaluateAccess(user Profile, resource Resource, level AccessLevel) Decision {
	if !resource.Allows(level) {
		return Decision{Reason: DenyLevelNotInCatalog}
	}
	if resource.IsProduction && user.Role == RoleIntern {
		return Decision{Reason: DenyProductionRestricted}
	}
	if resource.OwnerTeamID == user.TeamID {
		return Decision{Allowed: true}
	}
	if user.Role == RoleIntern && level == AccessRead {
		return Decision{Allowed: true}
	}
	if user.Role == RoleIntern {
		return Decision{Reason: DenyForeignTeamWrite}
	}
	return Decision{Reason: DenyForbidden}
}
