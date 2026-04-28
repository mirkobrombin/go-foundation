package serializer

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
)

// NamingStrategy controls how struct field names are transformed during serialization.
type NamingStrategy int

const (
	// PascalCase leaves names as-is (Go default).
	PascalCase NamingStrategy = iota
	// CamelCase converts to lowerCamelCase.
	CamelCase
	// SnakeCase converts to snake_case.
	SnakeCase
)

// ConverterFactory creates a custom type converter for serialization.
type ConverterFactory func() Converter

// Converter handles custom type serialization.
type Converter interface {
	Encode(v any) (any, error)
	Decode(v any) error
}

// Policy defines serialization behavior including naming and custom type handling.
type Policy struct {
	naming      NamingStrategy
	ignoreNil   bool
	ignoreZero  bool
	customTypes map[reflect.Type]ConverterFactory
	tagName     string
	mu          sync.RWMutex
}

// Option configures a Policy.
type Option func(*Policy)

// New creates a Policy with the given options.
func New(opts ...Option) *Policy {
	p := &Policy{
		naming:      PascalCase,
		tagName:     "json",
		customTypes: make(map[reflect.Type]ConverterFactory),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithNaming sets the naming strategy.
func WithNaming(n NamingStrategy) Option {
	return func(p *Policy) { p.naming = n }
}

// WithIgnoreNil skips nil pointers during marshaling.
func WithIgnoreNil() Option {
	return func(p *Policy) { p.ignoreNil = true }
}

// WithIgnoreZero skips zero values during marshaling.
func WithIgnoreZero() Option {
	return func(p *Policy) { p.ignoreZero = true }
}

// WithTagName sets the struct tag used for field names (default: "json").
func WithTagName(tag string) Option {
	return func(p *Policy) { p.tagName = tag }
}

// WithCustomType registers a custom converter for a specific type.
func WithCustomType(typ reflect.Type, factory ConverterFactory) Option {
	return func(p *Policy) { p.customTypes[typ] = factory }
}

// Marshal serializes v to JSON bytes using the policy.
func (p *Policy) Marshal(v any) ([]byte, error) {
	data, err := p.toMap(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(data)
}

// Unmarshal deserializes JSON bytes into v using the policy.
func (p *Policy) Unmarshal(data []byte, v any) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return p.fromMap(raw, v)
}

// MarshalToString serializes v to a JSON string.
func (p *Policy) MarshalToString(v any) (string, error) {
	data, err := p.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (p *Policy) toMap(v any) (map[string]any, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		result := make(map[string]any)
		result["value"] = v
		return result, nil
	}

	rt := rv.Type()
	result := make(map[string]any)

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		fv := rv.Field(i)

		if p.ignoreNil && fv.Kind() == reflect.Ptr && fv.IsNil() {
			continue
		}
		if p.ignoreZero && fv.IsZero() {
			continue
		}

		name := p.fieldName(field)
		result[name] = fv.Interface()
	}
	return result, nil
}

func (p *Policy) fromMap(raw map[string]any, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return json.Unmarshal(mustJSON(raw), v)
	}

	elem := rv.Elem()
	rt := elem.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		name := p.fieldName(field)
		if val, ok := raw[name]; ok {
			fv := elem.Field(i)
			if fv.CanSet() {
				setValue(fv, val)
			}
		}
	}
	return nil
}

func (p *Policy) fieldName(field reflect.StructField) string {
	if tag := field.Tag.Get(p.tagName); tag != "" {
		parts := strings.Split(tag, ",")
		if parts[0] != "" {
			return parts[0]
		}
	}
	return transformName(field.Name, p.naming)
}

func transformName(name string, strategy NamingStrategy) string {
	switch strategy {
	case SnakeCase:
		return toSnake(name)
	case CamelCase:
		return toCamel(name)
	default:
		return name
	}
}

func toSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

func toCamel(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func setValue(fv reflect.Value, val any) {
	if val == nil {
		return
	}
	rv := reflect.ValueOf(val)
	if rv.Type().ConvertibleTo(fv.Type()) {
		fv.Set(rv.Convert(fv.Type()))
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}