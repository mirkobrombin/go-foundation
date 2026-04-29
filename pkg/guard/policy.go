package guard

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/tags"
	"github.com/mirkobrombin/go-foundation/pkg/guard/checker"
)

var (
	globalParser = tags.NewParser("guard")
	policyCache  = sync.Map{}
)

// CompiledPolicy holds pre-compiled authorization rules for a resource type.
type CompiledPolicy struct {
	StaticRules    map[string]map[string]bool
	DynamicRules   []DynamicRule
	actionIndex    map[string][]int
	wildcardIndex  []int
}

// DynamicRule maps a dynamic role field to the actions it governs.
type DynamicRule struct {
	FieldIndex int
	Actions    []string
	FieldType  reflect.Type
	FieldKind   reflect.Kind
}

func getPolicy(typ reflect.Type) *CompiledPolicy {
	if val, ok := policyCache.Load(typ); ok {
		return val.(*CompiledPolicy)
	}
	policy := compilePolicy(typ)
	policyCache.Store(typ, policy)
	return policy
}

func compilePolicy(typ reflect.Type) *CompiledPolicy {
	fields := globalParser.ParseType(typ)

	policy := &CompiledPolicy{
		StaticRules:    make(map[string]map[string]bool),
		DynamicRules:   make([]DynamicRule, 0),
		actionIndex:    make(map[string][]int),
		wildcardIndex:  make([]int, 0),
	}

	for _, meta := range fields {
		permissions := meta.GetAll("can")
		roles := meta.GetAll("role")

		isDynamicRole := false
		staticRoles := make([]string, 0, len(roles))

		for _, r := range roles {
			if r == "*" {
				isDynamicRole = true
			} else {
				staticRoles = append(staticRoles, r)
			}
		}

		if len(permissions) > 0 {
			for _, action := range permissions {
				if len(staticRoles) > 0 {
					if policy.StaticRules[action] == nil {
						policy.StaticRules[action] = make(map[string]bool)
					}
					for _, r := range staticRoles {
						policy.StaticRules[action][r] = true
					}
				}
			}

			if isDynamicRole {
				dr := DynamicRule{
					FieldIndex: meta.Index,
					Actions:    permissions,
					FieldType:  meta.Type,
					FieldKind:  meta.Type.Kind(),
				}
				idx := len(policy.DynamicRules)
				policy.DynamicRules = append(policy.DynamicRules, dr)
				for _, action := range permissions {
					if action == "*" {
						policy.wildcardIndex = append(policy.wildcardIndex, idx)
					} else {
						policy.actionIndex[action] = append(policy.actionIndex[action], idx)
					}
				}
			}
		}
	}

	return policy
}

func (p *CompiledPolicy) Evaluate(user Identity, resourceVal reflect.Value, action string) error {
	allowed := false
	ruleFound := false

	checkStatic := func(act string) {
		if allowedRoles, ok := p.StaticRules[act]; ok {
			ruleFound = true
			userRoles := user.GetRoles()
			for _, ur := range userRoles {
				if allowedRoles[ur] {
					allowed = true
					return
				}
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

	ruleIndices := p.actionIndex[action]
	rRuleIndices := make([]int, len(ruleIndices))
	copy(rRuleIndices, ruleIndices)
	rRuleIndices = append(rRuleIndices, p.wildcardIndex...)

	for _, idx := range rRuleIndices {
		if idx >= len(p.DynamicRules) {
			continue
		}
		rule := p.DynamicRules[idx]
		ruleFound = true
		fieldVal := resourceVal.Field(rule.FieldIndex)
		dynamicRoles := extractRoles(fieldVal, user.GetID())
		userRoles := user.GetRoles()

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