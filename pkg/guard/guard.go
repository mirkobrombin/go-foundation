package guard

import (
	"errors"
	"reflect"
)

// Identity represents the actor trying to access a resource.
type Identity interface {
	GetID() string
	GetRoles() []string
}

// Guard provides the authorization engine.
type Guard struct{}

// NewGuard creates a new guard engine.
func NewGuard() *Guard {
	return &Guard{}
}

// GetRoles returns all roles resolved for the identity on the resource.
func (g *Guard) GetRoles(user Identity, resource any) ([]string, error) {
	if user == nil {
		return nil, errors.New("identity is nil")
	}
	if resource == nil {
		return nil, errors.New("resource is nil")
	}

	val := reflect.ValueOf(resource)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil, errors.New("resource must be a struct or pointer to struct")
	}

	// We need to support GetRoles even if the new policy logic is action-centric.
	// But `GetRoles` is about "What roles does this user have on this resource?".
	// The new Policy tracks logic per ACTION.
	// However, we can inspect the policy to see which roles match.
	// OR we can keep the old logic for GetRoles?
	// But optimizing GetRoles is also important if used frequently.
	// But `Can` is the main entry point.
	// Let's implement GetRoles by checking all possible roles in the policy against the user.

	policy := getPolicy(val.Type())

	// Collect matching roles
	matchedRoles := make(map[string]bool)

	// Check user's explicit roles against any static role used in policy?
	// User has roles [A, B].
	// If resource allows A for action Read, then user has role A on this resource.
	// This is vague.
	// `GetRoles` usually returns roles that match dynamic criteria + static ones?
	// Original logic: "If `role:admin` is on field, and user has `admin` global role, then user has `admin` on resource."

	userRoles := user.GetRoles()
	userRolesMap := make(map[string]bool, len(userRoles))
	for _, r := range userRoles {
		userRolesMap[r] = true
	}

	// 1. Static Roles from Policy
	// We don't have a simple list of "All Static Roles" in compiled policy, but we have them in `StaticRules`.
	for _, roleMap := range policy.StaticRules {
		for r := range roleMap {
			if r == "*" {
				continue
			}
			if userRolesMap[r] {
				matchedRoles[r] = true
			}
		}
	}

	// 2. Dynamic Roles
	for _, rule := range policy.DynamicRules {
		fieldVal := val.Field(rule.FieldIndex)
		roles := extractRoles(fieldVal, user.GetID())
		for _, r := range roles {
			// If dynamic role (e.g. from DB) matches user's ID or is in user's roles?
			// `role:*` extraction logic in original checked `fieldVal` against `user.GetID()`.
			// Wait, the new `extractRoles` logic I wrote uses `checker.IsMatch(key, userID)`.
			// So it extracts roles if the KEY matches the user.
			// e.g. `role:*` on `map[string]string`. User ID "123". Map["123"] = "owner".
			// Then "owner" is extracted.
			// Then we check if user HAS role "owner"?
			// Original logic: `userRoles[roleVal.String()] = true`.
			// Yes, so if extracted role is in User's roles, we add it. Or is the extracted string THE role the user has?

			// If `role:*` resolves to "owner", it means "The user with ID matching the key HAS the role 'owner' on this resource".
			// So we add "owner" to `matchedRoles`.
			// We DO NOT check if user already has "owner" in global traits.
			// Validated against original lines 92: `userRoles[roleVal.String()] = true`.

			matchedRoles[r] = true
		}
	}

	roles := make([]string, 0, len(matchedRoles))
	for r := range matchedRoles {
		roles = append(roles, r)
	}
	return roles, nil
}

// Can checks if the identity is allowed to perform the action on the resource.
func (g *Guard) Can(user Identity, resource any, action string) error {
	if user == nil {
		return errors.New("identity is nil")
	}
	if resource == nil {
		return errors.New("resource is nil")
	}

	val := reflect.ValueOf(resource)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return errors.New("resource must be a struct or pointer to struct")
	}

	policy := getPolicy(val.Type())
	return policy.Evaluate(user, val, action)
}

// Can checks if the identity is allowed to perform the action on the resource.
// Deprecated: use Guard.Can instead.
func Can(user Identity, resource any, action string) error {
	return NewGuard().Can(user, resource, action)
}
