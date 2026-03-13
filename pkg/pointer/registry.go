package pointer

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// FieldInfo holds identity and tag metadata for a struct field.
type FieldInfo struct {
	Name   string              // Go struct field name
	Offset uintptr             // Memory offset from struct base
	Type   reflect.Type        // Field type
	Tags   map[string][]string // Parsed tags for the configured tagName
}

type typeMap struct {
	fields []FieldInfo
	byOff  map[uintptr]*FieldInfo
}

// Registry manages the mapping of struct types to their field offsets.
type Registry struct {
	tagName string
	parser  *tags.Parser
	mu      sync.RWMutex
	cache   map[reflect.Type]*typeMap
}

// NewRegistry creates a new Registry that parses the given tagName.
func NewRegistry(tagName string) *Registry {
	return &Registry{
		tagName: tagName,
		parser: tags.NewParser(tagName,
			tags.WithPairDelimiter(";"),
			tags.WithKVSeparator(":"),
			tags.WithValueDelimiter(","),
		),
		cache: make(map[reflect.Type]*typeMap),
	}
}

// Register inspects a struct type and builds the field mapping.
func (r *Registry) Register(v any) {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("pointer.Register: expected struct, got %s", t.Kind()))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.cache[t]; ok {
		return
	}

	tm := &typeMap{byOff: make(map[uintptr]*FieldInfo)}

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}

		fi := FieldInfo{
			Name:   sf.Name,
			Offset: sf.Offset,
			Type:   sf.Type,
		}

		if r.tagName != "" {
			tagStr := sf.Tag.Get(r.tagName)
			if tagStr != "" {
				fi.Tags = r.parser.Parse(tagStr)
			}
		}

		tm.fields = append(tm.fields, fi)
		tm.byOff[sf.Offset] = &tm.fields[len(tm.fields)-1]
	}

	r.cache[t] = tm
}

// Resolve retrieves field info for a given base struct and field pointer.
func (r *Registry) Resolve(base any, fieldPtr any) (*FieldInfo, error) {
	bv := reflect.ValueOf(base)
	if bv.Kind() != reflect.Ptr || bv.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("base must be a pointer to a struct")
	}

	fv := reflect.ValueOf(fieldPtr)
	if fv.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("fieldPtr must be a pointer")
	}

	baseAddr := bv.Pointer()
	fieldAddr := fv.Pointer()
	offset := fieldAddr - baseAddr

	r.mu.RLock()
	tm, ok := r.cache[bv.Elem().Type()]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("type %s not registered", bv.Elem().Type())
	}

	fi, ok := tm.byOff[offset]
	if !ok {
		return nil, fmt.Errorf("offset %d not found in type %s", offset, bv.Elem().Type())
	}

	return fi, nil
}

// FieldName returns the Go field name for a field pointer.
func FieldName[B any, F any](r *Registry, base *B, fieldPtr *F) string {
	fi, err := r.Resolve(base, fieldPtr)
	if err != nil {
		panic(fmt.Sprintf("pointer.FieldName: %v", err))
	}
	return fi.Name
}

// TagValue returns the first value of a tag key for a field pointer.
func TagValue[B any, F any](r *Registry, base *B, fieldPtr *F, key string) string {
	fi, err := r.Resolve(base, fieldPtr)
	if err != nil {
		panic(fmt.Sprintf("pointer.TagValue: %v", err))
	}
	if vals, ok := fi.Tags[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// HasTag returns true if the tag key exists for a field pointer.
func HasTag[B any, F any](r *Registry, base *B, fieldPtr *F, key string) bool {
	fi, err := r.Resolve(base, fieldPtr)
	if err != nil {
		panic(fmt.Sprintf("pointer.HasTag: %v", err))
	}
	_, ok := fi.Tags[key]
	return ok
}
