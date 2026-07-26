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

	policy := getPolicy(val.Type())
	matchedRoles := make(map[string]bool)

	userRoles := user.GetRoles()
	userRolesMap := make(map[string]bool, len(userRoles))
	for _, r := range userRoles {
		userRolesMap[r] = true
	}

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

	for _, rule := range policy.DynamicRules {
		fieldVal := val.Field(rule.FieldIndex)
		roles := extractRoles(fieldVal, user.GetID())
		for _, r := range roles {
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
