package guard

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/tags"
	"github.com/mirkobrombin/go-foundation/pkg/guard/checker"
)

var (
	// globalParser is the shared tag parser.
	// We use the default configuration: pairDelim=";", kvSep=":", valueDelim=","
	globalParser = tags.NewParser("guard")

	// policyCache stores compiled policies for types to avoid re-parsing.
	policyCache = sync.Map{} // map[reflect.Type]*CompiledPolicy
)

// CompiledPolicy represents the evaluated security rules for a specific type.
type CompiledPolicy struct {
	// StaticRules maps action -> required roles (union of roles from all fields covering the action).
	// If the list is empty, it means the action is allowed for everyone?
	// Or allowed for no one? logic: if field defines action, it also defines roles.
	// If multiple fields define same action, any of them is sufficient (OR logic).
	StaticRules map[string]map[string]bool

	// DynamicRules stores rules that require runtime evaluation (e.g. role checking against field value).
	DynamicRules []DynamicRule
}

// DynamicRule represents a rule dependent on a field's value.
type DynamicRule struct {
	FieldIndex int
	Actions    []string // Actions this rule covers.
	IsWildcard bool     // If true, field value is treated as a role for ANY action (or specific actions).
	// Actually, "role:*" means the role name comes from the field value.
	// "can:read" means this rule applies to Read.
}

func getPolicy(typ reflect.Type) *CompiledPolicy {
	if val, ok := policyCache.Load(typ); ok {
		return val.(*CompiledPolicy)
	}

	// Double check
	// sync.Map doesn't have double check locking built-in for LoadOrStore construction,
	// but it's safe to compute twice and overwrite.

	policy := compilePolicy(typ)
	policyCache.Store(typ, policy)
	return policy
}

func compilePolicy(typ reflect.Type) *CompiledPolicy {
	// Use the global parser (which has its own cache for FieldMeta, but we go further)
	fields := globalParser.ParseType(typ)

	policy := &CompiledPolicy{
		StaticRules:  make(map[string]map[string]bool),
		DynamicRules: make([]DynamicRule, 0),
	}

	for _, meta := range fields {
		permissions := meta.GetAll("can") // Actions
		roles := meta.GetAll("role")      // Roles

		// Check for dynamic roles
		isDynamicRole := false
		staticRoles := make([]string, 0, len(roles))

		for _, r := range roles {
			if r == "*" {
				isDynamicRole = true
			} else {
				staticRoles = append(staticRoles, r)
			}
		}

		// If we have permissions, we map them
		if len(permissions) > 0 {
			for _, action := range permissions {
				// Static roles part
				if len(staticRoles) > 0 {
					if policy.StaticRules[action] == nil {
						policy.StaticRules[action] = make(map[string]bool)
					}
					// Also "wildcard action" handling?
					// If action is "*", it applies to ALL actions requested runtime.
					// We store it as "*" key.

					for _, r := range staticRoles {
						policy.StaticRules[action][r] = true
					}
				}
			}

			// Dynamic roles part
			if isDynamicRole {
				policy.DynamicRules = append(policy.DynamicRules, DynamicRule{
					FieldIndex: meta.Index,
					Actions:    permissions,
					IsWildcard: false,
				})
			}
		}
	}

	return policy
}

// Evaluate checks if the user has permission for the action on the resource value.
func (p *CompiledPolicy) Evaluate(user Identity, resourceVal reflect.Value, action string) error {
	// 1. Check Static Rules
	// We need to check exact action match AND wildcard "*" action match.

	allowed := false
	ruleFound := false

	// Helper to check map
	checkStatic := func(act string) {
		if allowedRoles, ok := p.StaticRules[act]; ok {
			ruleFound = true
			// Check if user has any of these roles
			userRoles := user.GetRoles()
			for _, ur := range userRoles {
				if allowedRoles[ur] {
					allowed = true
					return
				}
				// Check if allowedRoles has "*" (wildcard role allowed?)
				if allowedRoles["*"] {
					allowed = true
					return
				}
			}
		}
	}

	checkStatic(action)
	if allowed {
		return nil
	}
	checkStatic("*")
	if allowed {
		return nil
	}

	// 2. Check Dynamic Rules
	for _, rule := range p.DynamicRules {
		// Check if rule applies to this action
		actionMatch := false
		for _, a := range rule.Actions {
			if a == action || a == "*" {
				actionMatch = true
				break
			}
		}

		if actionMatch {
			ruleFound = true
			fieldVal := resourceVal.Field(rule.FieldIndex)
			dynamicRoles := extractRoles(fieldVal, user.GetID())
			userRoles := user.GetRoles()

			// Check intersection
			for _, dr := range dynamicRoles {
				for _, ur := range userRoles {
					if dr == ur {
						allowed = true
						break
					}
				}
				if allowed {
					break
				}
			}
		}
		if allowed {
			break
		}
	}

	if !ruleFound {
		return fmt.Errorf("no policy defined for action '%s'", action)
	}
	if !allowed {
		return fmt.Errorf("permission denied for action '%s'", action)
	}

	return nil
}

func extractRoles(val reflect.Value, userID string) []string {
	var roles []string
	// Handle Map, String, Slice
	if val.Kind() == reflect.Map {
		for _, key := range val.MapKeys() {
			if checker.IsMatch(key, userID) {
				roleVal := val.MapIndex(key)
				if roleVal.Kind() == reflect.String {
					roles = append(roles, roleVal.String())
				} else if roleVal.Kind() == reflect.Slice || roleVal.Kind() == reflect.Array {
					for i := 0; i < roleVal.Len(); i++ {
						rv := roleVal.Index(i)
						if rv.Kind() == reflect.String {
							roles = append(roles, rv.String())
						}
					}
				}
			}
		}
	} else if val.Kind() == reflect.String {
		roles = append(roles, val.String())
	} else if val.Kind() == reflect.Slice {
		for i := 0; i < val.Len(); i++ {
			rv := val.Index(i)
			if rv.Kind() == reflect.String {
				roles = append(roles, rv.String())
			}
		}
	}
	return roles
}
