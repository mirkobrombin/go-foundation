package validation

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// Rule validates a field value given an optional parameter.
type Rule func(value reflect.Value, param string) error

type compiledField struct {
	Index int
	Rules []compiledRule
}

type compiledRule struct {
	Name  string
	Rule  Rule
	Param string
}

type typeCache struct {
	fields []compiledField
}

// Validator validates structs based on "validate" struct tags.
type Validator struct {
	mu         sync.RWMutex
	rules      map[string]Rule
	emailRegex *regexp.Regexp
	cache      map[string]*regexp.Regexp
	cacheMu    sync.RWMutex
	typeCache  map[reflect.Type]*typeCache
	typeMu     sync.RWMutex
}

// Error represents a single field validation error.
type Error struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Errors is a collection of validation errors.
type Errors []Error

func (e Errors) Error() string {
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// New creates a new Validator with built-in rules.
func New() *Validator {
	v := &Validator{
		rules:      make(map[string]Rule),
		emailRegex: regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`),
		cache:      make(map[string]*regexp.Regexp),
		typeCache:  make(map[reflect.Type]*typeCache),
	}
	v.registerBuiltin()
	return v
}

func (v *Validator) Register(name string, rule Rule) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.rules[name] = rule
}

var tagParser = tags.NewParser("validate", tags.WithPairDelimiter(";"))

func (v *Validator) Validate(target any) Errors {
	val := reflect.ValueOf(target)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return Errors{{Field: "", Message: "target must be a struct"}}
	}

	typ := val.Type()
	v.typeMu.RLock()
	tc, ok := v.typeCache[typ]
	v.typeMu.RUnlock()

	if !ok {
		v.typeMu.Lock()
		tc, ok = v.typeCache[typ]
		if !ok {
			tc = v.compileType(typ, target)
			v.typeCache[typ] = tc
		}
		v.typeMu.Unlock()
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	var errs Errors
	for _, cf := range tc.fields {
		fieldVal := val.Field(cf.Index)
		for _, cr := range cf.Rules {
			if err := cr.Rule(fieldVal, cr.Param); err != nil {
				errs = append(errs, Error{
					Field:   typ.Field(cf.Index).Name,
					Message: err.Error(),
				})
			}
		}
	}
	return errs
}

func (v *Validator) compileType(typ reflect.Type, target any) *typeCache {
	fields := tagParser.ParseStruct(target)
	v.mu.RLock()
	defer v.mu.RUnlock()

	tc := &typeCache{}
	for _, meta := range fields {
		tag := meta.RawTag
		if tag == "" {
			continue
		}
		cf := compiledField{Index: meta.Index}
		rules := strings.Split(tag, ",")
		for _, ruleDef := range rules {
			ruleDef = strings.TrimSpace(ruleDef)
			if ruleDef == "" {
				continue
			}
			var ruleName, param string
			if idx := strings.Index(ruleDef, "="); idx != -1 {
				ruleName = ruleDef[:idx]
				param = ruleDef[idx+1:]
			} else {
				ruleName = ruleDef
			}
			if rule, ok := v.rules[ruleName]; ok {
				cf.Rules = append(cf.Rules, compiledRule{Name: ruleName, Rule: rule, Param: param})
			}
		}
		if len(cf.Rules) > 0 {
			tc.fields = append(tc.fields, cf)
		}
	}
	return tc
}

func (v *Validator) registerBuiltin() {
	v.rules["required"] = func(field reflect.Value, _ string) error {
		switch field.Kind() {
		case reflect.String:
			if field.String() == "" {
				return fmt.Errorf("required")
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		case reflect.Float32, reflect.Float64:
		case reflect.Slice, reflect.Map:
			if field.Len() == 0 {
				return fmt.Errorf("required")
			}
		case reflect.Bool:
		default:
			if field.IsZero() {
				return fmt.Errorf("required")
			}
		}
		return nil
	}

	v.rules["min"] = func(field reflect.Value, param string) error {
		switch field.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			min, err := strconv.ParseInt(param, 10, 64)
			if err != nil {
				return nil
			}
			if field.Int() < min {
				return fmt.Errorf("min %v", min)
			}
		case reflect.Float32, reflect.Float64:
			min, err := strconv.ParseFloat(param, 64)
			if err != nil {
				return nil
			}
			if field.Float() < min {
				return fmt.Errorf("min %v", min)
			}
		}
		return nil
	}

	v.rules["max"] = func(field reflect.Value, param string) error {
		switch field.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			max, err := strconv.ParseInt(param, 10, 64)
			if err != nil {
				return nil
			}
			if field.Int() > max {
				return fmt.Errorf("max %v", max)
			}
		case reflect.Float32, reflect.Float64:
			max, err := strconv.ParseFloat(param, 64)
			if err != nil {
				return nil
			}
			if field.Float() > max {
				return fmt.Errorf("max %v", max)
			}
		}
		return nil
	}

	v.rules["email"] = func(field reflect.Value, _ string) error {
		if field.Kind() != reflect.String {
			return nil
		}
		if !v.emailRegex.MatchString(field.String()) {
			return fmt.Errorf("invalid email")
		}
		return nil
	}

	v.rules["pattern"] = func(field reflect.Value, param string) error {
		if field.Kind() != reflect.String {
			return nil
		}
		v.cacheMu.RLock()
		re, ok := v.cache[param]
		v.cacheMu.RUnlock()
		if !ok {
			v.cacheMu.Lock()
			re, ok = v.cache[param]
			if !ok {
				var err error
				re, err = regexp.Compile(param)
				if err != nil {
					v.cacheMu.Unlock()
					return nil
				}
				v.cache[param] = re
			}
			v.cacheMu.Unlock()
		}
		if !re.MatchString(field.String()) {
			return fmt.Errorf("pattern mismatch")
		}
		return nil
	}
}
