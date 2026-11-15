package domain

import (
	"errors"
	"fmt"
)

// Role is the user role from the trusted test identity header.
type Role string

const (
	RoleIntern Role = "intern"
	RoleMentor Role = "mentor"
	RoleLead   Role = "lead"
)

func (r Role) Valid() bool {
	switch r {
	case RoleIntern, RoleMentor, RoleLead:
		return true
	default:
		return false
	}
}

// Profile is reference data, not a persisted user account.
type Profile struct {
	UserID       UserID
	Name         string
	Role         Role
	TeamID       TeamID
	MentorHandle string
}

func (p Profile) Validate() error {
	if err := p.UserID.Validate(); err != nil {
		return err
	}
	if err := requireNonEmpty("name", p.Name); err != nil {
		return err
	}
	if !p.Role.Valid() {
		return &ValidationError{Field: "role", Message: fmt.Sprintf("unsupported role %q", p.Role)}
	}
	return p.TeamID.Validate()
}

// Team owns resources and defines the approval route through its lead.
type Team struct {
	ID     TeamID
	Name   string
	LeadID UserID
}

func (t Team) Validate() error {
	if err := t.ID.Validate(); err != nil {
		return err
	}
	return requireNonEmpty("team.name", t.Name)
}

// UserView is the domain projection of GET /context's user object. Team name
// and approver are resolved from the directory instead of being duplicated in
// each Profile.
type UserView struct {
	UserID       UserID
	Name         string
	Role         Role
	TeamID       TeamID
	TeamName     string
	MentorHandle string
	ApproverName string
}

// Directory is an immutable in-memory reference-data directory.
type Directory struct {
	profiles     map[UserID]Profile
	teams        map[TeamID]Team
	profileOrder []UserID
	teamOrder    []TeamID
}

func NewDirectory(profiles []Profile, teams []Team) (Directory, error) {
	d := Directory{
		profiles:     make(map[UserID]Profile, len(profiles)),
		teams:        make(map[TeamID]Team, len(teams)),
		profileOrder: make([]UserID, 0, len(profiles)),
		teamOrder:    make([]TeamID, 0, len(teams)),
	}

	for _, team := range teams {
		if err := team.Validate(); err != nil {
			return Directory{}, err
		}
		if _, exists := d.teams[team.ID]; exists {
			return Directory{}, fmt.Errorf("%w: duplicate team %q", ErrValidation, team.ID)
		}
		d.teams[team.ID] = team
		d.teamOrder = append(d.teamOrder, team.ID)
	}

	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return Directory{}, err
		}
		if _, exists := d.profiles[profile.UserID]; exists {
			return Directory{}, fmt.Errorf("%w: duplicate profile %q", ErrValidation, profile.UserID)
		}
		if _, exists := d.teams[profile.TeamID]; !exists {
			return Directory{}, &NotFoundError{Entity: "team", ID: string(profile.TeamID)}
		}
		d.profiles[profile.UserID] = profile
		d.profileOrder = append(d.profileOrder, profile.UserID)
	}

	for _, team := range teams {
		if team.LeadID == "" {
			continue
		}
		if _, exists := d.profiles[team.LeadID]; !exists {
			return Directory{}, &NotFoundError{Entity: "team lead", ID: string(team.LeadID)}
		}
	}

	return d, nil
}

func (d Directory) Profile(id UserID) (Profile, error) {
	profile, ok := d.profiles[id]
	if !ok {
		return Profile{}, &NotFoundError{Entity: "profile", ID: string(id)}
	}
	return profile, nil
}

func (d Directory) Team(id TeamID) (Team, error) {
	team, ok := d.teams[id]
	if !ok {
		return Team{}, &NotFoundError{Entity: "team", ID: string(id)}
	}
	return team, nil
}

func (d Directory) TeamName(id TeamID) (string, error) {
	team, err := d.Team(id)
	if err != nil {
		return "", err
	}
	return team.Name, nil
}

// ApproverFor returns the team lead who approves requests of this profile. A
// lead does not have a separate approver in MVP reference data.
func (d Directory) ApproverFor(id UserID) (Profile, error) {
	profile, err := d.Profile(id)
	if err != nil {
		return Profile{}, err
	}
	team, err := d.Team(profile.TeamID)
	if err != nil {
		return Profile{}, err
	}
	if team.LeadID == "" || team.LeadID == profile.UserID {
		return Profile{}, &NotFoundError{Entity: "approver", ID: string(id)}
	}
	return d.Profile(team.LeadID)
}

func (d Directory) UserView(id UserID) (UserView, error) {
	profile, err := d.Profile(id)
	if err != nil {
		return UserView{}, err
	}
	team, err := d.Team(profile.TeamID)
	if err != nil {
		return UserView{}, err
	}

	approverName := ""
	if approver, err := d.ApproverFor(id); err == nil {
		approverName = approver.Name
	} else if !isNotFound(err) {
		return UserView{}, err
	}

	return UserView{
		UserID:       profile.UserID,
		Name:         profile.Name,
		Role:         profile.Role,
		TeamID:       profile.TeamID,
		TeamName:     team.Name,
		MentorHandle: profile.MentorHandle,
		ApproverName: approverName,
	}, nil
}

func (d Directory) Profiles() []Profile {
	out := make([]Profile, 0, len(d.profileOrder))
	for _, id := range d.profileOrder {
		out = append(out, d.profiles[id])
	}
	return out
}

func (d Directory) Teams() []Team {
	out := make([]Team, 0, len(d.teamOrder))
	for _, id := range d.teamOrder {
		out = append(out, d.teams[id])
	}
	return out
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
