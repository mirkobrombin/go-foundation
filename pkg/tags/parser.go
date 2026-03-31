package tags

import (
	"reflect"
	"strings"
	"sync"
)

// Parser provides generic struct tag parsing with configurable syntax.
//
// Example:
//
//	p := tags.NewParser("my-tag", tags.WithPairDelimiter(";"))
//	data := p.Parse("key:val; option:a,b")
type Parser struct {
	tagName         string
	pairDelimiter   string
	kvSeparator     string
	valueDelim      string
	includeUntagged bool
	cache           map[reflect.Type][]FieldMeta
	mu              sync.RWMutex
}

// Option configures a Parser.
type Option func(*Parser)

// WithIncludeUntagged configures the parser to include fields even if the tag is missing.
func WithIncludeUntagged() Option {
	return func(p *Parser) { p.includeUntagged = true }
}

// WithPairDelimiter sets the delimiter between key:value pairs (default ";").
func WithPairDelimiter(d string) Option {
	return func(p *Parser) { p.pairDelimiter = d }
}

// WithKVSeparator sets the key-value separator (default ":").
func WithKVSeparator(s string) Option {
	return func(p *Parser) { p.kvSeparator = s }
}

// WithValueDelimiter sets the delimiter for multiple values (default ",").
func WithValueDelimiter(d string) Option {
	return func(p *Parser) { p.valueDelim = d }
}

// NewParser creates a Parser for the given tag name.
func NewParser(tagName string, opts ...Option) *Parser {
	p := &Parser{
		tagName:       tagName,
		pairDelimiter: ";",
		kvSeparator:   ":",
		valueDelim:    ",",
		cache:         make(map[reflect.Type][]FieldMeta),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Parse extracts key-value pairs from a tag string.
func (p *Parser) Parse(tag string) map[string][]string {
	result := make(map[string][]string)

	for part := range strings.SplitSeq(tag, p.pairDelimiter) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		key, value, found := strings.Cut(part, p.kvSeparator)
		key = strings.TrimSpace(key)

		if !found {
			result[key] = nil
			continue
		}

		value = strings.TrimSpace(value)
		var values []string
		for v := range strings.SplitSeq(value, p.valueDelim) {
			values = append(values, strings.TrimSpace(v))
		}
		result[key] = values
	}

	return result
}

// FieldMeta holds parsed tag metadata for a struct field.
type FieldMeta struct {
	Name       string
	Index      int
	Type       reflect.Type
	Tags       map[string][]string
	RawTag     string
	IsExported bool
}

// ParseStruct extracts tag metadata from all fields of a struct.
func (p *Parser) ParseStruct(v any) []FieldMeta {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}
	return p.ParseType(val.Type())
}

// ParseType extracts tag metadata from a reflect.Type.
func (p *Parser) ParseType(typ reflect.Type) []FieldMeta {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}

	p.mu.RLock()
	if cached, ok := p.cache[typ]; ok {
		p.mu.RUnlock()
		return cached
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check logic
	if cached, ok := p.cache[typ]; ok {
		return cached
	}

	var fields []FieldMeta

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get(p.tagName)
		if tag == "" && !p.includeUntagged {
			continue
		}

		var parsedTags map[string][]string
		if tag != "" {
			parsedTags = p.Parse(tag)
		}

		fields = append(fields, FieldMeta{
			Name:       field.Name,
			Index:      i,
			Type:       field.Type,
			Tags:       parsedTags,
			RawTag:     tag,
			IsExported: field.IsExported(),
		})
	}

	p.cache[typ] = fields
	return fields
}

// Get returns the first value for a key, or empty string if not found.
func (m FieldMeta) Get(key string) string {
	if vals, ok := m.Tags[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// GetAll returns all values for a key.
func (m FieldMeta) GetAll(key string) []string {
	return m.Tags[key]
}

// Has checks if a key exists in the tag.
func (m FieldMeta) Has(key string) bool {
	_, ok := m.Tags[key]
	return ok
}
