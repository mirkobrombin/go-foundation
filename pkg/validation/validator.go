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

type Rule func(value reflect.Value, param string) error

type Validator struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

type Error struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

type Errors []Error

func (e Errors) Error() string {
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

func New() *Validator {
	v := &Validator{rules: make(map[string]Rule)}
	v.registerBuiltin()
	return v
}

func (v *Validator) Register(name string, rule Rule) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.rules[name] = rule
}

func (v *Validator) Validate(target any) Errors {
	val := reflect.ValueOf(target)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return Errors{{Field: "", Message: "target must be a struct"}}
	}

	var errs Errors
	parser := tags.NewParser("validate", tags.WithPairDelimiter(","))
	fields := parser.ParseStruct(target)

	v.mu.RLock()
	defer v.mu.RUnlock()

	for _, meta := range fields {
		fieldVal := val.Field(meta.Index)
		tag := meta.RawTag
		if tag == "" {
			continue
		}
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
				if err := rule(fieldVal, param); err != nil {
					errs = append(errs, Error{
						Field:   meta.Name,
						Message: err.Error(),
					})
				}
			}
		}
	}
	return errs
}

func (v *Validator) registerBuiltin() {
	v.rules["required"] = func(field reflect.Value, _ string) error {
		switch field.Kind() {
		case reflect.String:
			if field.String() == "" {
				return fmt.Errorf("required")
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if field.Int() == 0 {
				return fmt.Errorf("required")
			}
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
		min, err := strconv.ParseFloat(param, 64)
		if err != nil {
			return nil
		}
		switch field.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if float64(field.Int()) < min {
				return fmt.Errorf("min %v", min)
			}
		case reflect.Float32, reflect.Float64:
			if field.Float() < min {
				return fmt.Errorf("min %v", min)
			}
		}
		return nil
	}

	v.rules["max"] = func(field reflect.Value, param string) error {
		max, err := strconv.ParseFloat(param, 64)
		if err != nil {
			return nil
		}
		switch field.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if float64(field.Int()) > max {
				return fmt.Errorf("max %v", max)
			}
		case reflect.Float32, reflect.Float64:
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
		re := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
		if !re.MatchString(field.String()) {
			return fmt.Errorf("invalid email")
		}
		return nil
	}

	v.rules["pattern"] = func(field reflect.Value, param string) error {
		if field.Kind() != reflect.String {
			return nil
		}
		re, err := regexp.Compile(param)
		if err != nil {
			return nil
		}
		if !re.MatchString(field.String()) {
			return fmt.Errorf("pattern mismatch")
		}
		return nil
	}
}
