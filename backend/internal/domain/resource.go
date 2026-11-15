package domain

import "fmt"

// AccessLevel is a catalog level. "admin" exists in the catalog but is never
// requestable through the LLM tool or the propose API.
type AccessLevel string

const (
	AccessRead  AccessLevel = "read"
	AccessWrite AccessLevel = "write"
	AccessAdmin AccessLevel = "admin"
)

func (l AccessLevel) Valid() bool {
	switch l {
	case AccessRead, AccessWrite, AccessAdmin:
		return true
	default:
		return false
	}
}

// Requestable reports whether a level may be requested by a user. Admin access
// is provisioned outside the access-request flow.
func (l AccessLevel) Requestable() bool {
	return l == AccessRead || l == AccessWrite
}

// Resource is one entry of the fixed resource catalog.
type Resource struct {
	ID           ResourceID
	Title        string
	OwnerTeamID  TeamID
	Levels       []AccessLevel
	IsProduction bool
}

func (r Resource) Validate() error {
	if err := r.ID.Validate(); err != nil {
		return err
	}
	if err := requireNonEmpty("resource.title", r.Title); err != nil {
		return err
	}
	if err := r.OwnerTeamID.Validate(); err != nil {
		return err
	}
	if len(r.Levels) == 0 {
		return &ValidationError{Field: "resource.levels", Message: "must not be empty"}
	}
	seen := make(map[AccessLevel]struct{}, len(r.Levels))
	for _, level := range r.Levels {
		if !level.Valid() {
			return &ValidationError{
				Field:   "resource.levels",
				Message: fmt.Sprintf("unsupported access level %q", level),
			}
		}
		if _, exists := seen[level]; exists {
			return &ValidationError{
				Field:   "resource.levels",
				Message: fmt.Sprintf("duplicate access level %q", level),
			}
		}
		seen[level] = struct{}{}
	}
	return nil
}

// Allows reports whether the level is present in the resource catalog. Catalog
// membership takes priority over the generic read/write enum.
func (r Resource) Allows(level AccessLevel) bool {
	for _, allowed := range r.Levels {
		if allowed == level {
			return true
		}
	}
	return false
}

// Catalog is an immutable resource catalog.
type Catalog struct {
	resources map[ResourceID]Resource
	order     []ResourceID
}

func NewCatalog(resources []Resource) (Catalog, error) {
	c := Catalog{
		resources: make(map[ResourceID]Resource, len(resources)),
		order:     make([]ResourceID, 0, len(resources)),
	}
	for _, resource := range resources {
		if err := resource.Validate(); err != nil {
			return Catalog{}, err
		}
		if _, exists := c.resources[resource.ID]; exists {
			return Catalog{}, fmt.Errorf("%w: duplicate resource %q", ErrValidation, resource.ID)
		}
		c.resources[resource.ID] = resource
		c.order = append(c.order, resource.ID)
	}
	return c, nil
}

func (c Catalog) Resource(id ResourceID) (Resource, error) {
	resource, ok := c.resources[id]
	if !ok {
		return Resource{}, &NotFoundError{Entity: "resource", ID: string(id)}
	}
	return resource, nil
}

func (c Catalog) Resources() []Resource {
	out := make([]Resource, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.resources[id])
	}
	return out
}
