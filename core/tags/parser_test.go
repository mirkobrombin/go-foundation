package tags

import (
	"reflect"
	"testing"
)

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name     string
		tagName  string
		opts     []Option
		input    string
		expected map[string][]string
	}{
		{
			name:    "guard style",
			tagName: "guard",
			input:   "role:owner; read:admin,user; delete:admin",
			expected: map[string][]string{
				"role":   {"owner"},
				"read":   {"admin", "user"},
				"delete": {"admin"},
			},
		},
		{
			name:    "fsm style",
			tagName: "fsm",
			input:   "initial:draft; draft->paid; paid->shipped",
			expected: map[string][]string{
				"initial":       {"draft"},
				"draft->paid":   nil,
				"paid->shipped": nil,
			},
		},
		{
			name:    "conf style",
			tagName: "conf",
			opts:    []Option{WithPairDelimiter(",")},
			input:   "env:PORT,flag:port,default:8080",
			expected: map[string][]string{
				"env":     {"PORT"},
				"flag":    {"port"},
				"default": {"8080"},
			},
		},
		{
			name:     "empty tag",
			tagName:  "test",
			input:    "",
			expected: map[string][]string{},
		},
		{
			name:    "key without value",
			tagName: "test",
			input:   "required; optional",
			expected: map[string][]string{
				"required": nil,
				"optional": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(tt.tagName, tt.opts...)
			result := p.Parse(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d keys, want %d", len(result), len(tt.expected))
			}

			for k, want := range tt.expected {
				got, ok := result[k]
				if !ok {
					t.Errorf("missing key %q", k)
					continue
				}
				if len(got) != len(want) {
					t.Errorf("key %q: got %v, want %v", k, got, want)
				}
				for i := range want {
					if i >= len(got) || got[i] != want[i] {
						t.Errorf("key %q value %d: got %q, want %q", k, i, got[i], want[i])
					}
				}
			}
		})
	}
}

func TestParser_ParseStruct(t *testing.T) {
	type TestStruct struct {
		Untagged string
		Role     string `guard:"role:owner"`
		Perms    string `guard:"read:admin,user; write:owner"`
	}

	p := NewParser("guard")
	fields := p.ParseStruct(&TestStruct{})

	if len(fields) != 2 {
		t.Fatalf("got %d fields, want 2", len(fields))
	}

	if fields[0].Name != "Role" {
		t.Errorf("first field name: got %q, want %q", fields[0].Name, "Role")
	}
	if fields[0].Index != 1 {
		t.Errorf("Role field index: got %d, want 1", fields[0].Index)
	}

	if fields[0].Get("role") != "owner" {
		t.Errorf("Role field 'role' tag: got %q, want %q", fields[0].Get("role"), "owner")
	}

	if !fields[1].Has("read") {
		t.Error("Perms field should have 'read' key")
	}
	if fields[1].Index != 2 {
		t.Errorf("Perms field index: got %d, want 2", fields[1].Index)
	}

	perms := fields[1].GetAll("read")
	if len(perms) != 2 || perms[0] != "admin" || perms[1] != "user" {
		t.Errorf("Perms 'read' values: got %v, want [admin user]", perms)
	}
}

func TestParser_ParseType(t *testing.T) {
	type TestStruct struct {
		Status string `fsm:"initial:draft; draft->paid"`
	}

	p := NewParser("fsm")
	fields := p.ParseType(reflect.TypeOf(TestStruct{}))

	if len(fields) != 1 {
		t.Fatalf("got %d fields, want 1", len(fields))
	}

	if fields[0].Get("initial") != "draft" {
		t.Errorf("initial: got %q, want %q", fields[0].Get("initial"), "draft")
	}
}

func TestFieldMeta_Methods(t *testing.T) {
	meta := FieldMeta{
		Tags: map[string][]string{
			"single":   {"value"},
			"multiple": {"a", "b", "c"},
			"empty":    nil,
		},
	}

	if meta.Get("single") != "value" {
		t.Errorf("Get single: got %q, want %q", meta.Get("single"), "value")
	}

	if meta.Get("missing") != "" {
		t.Errorf("Get missing: got %q, want empty", meta.Get("missing"))
	}

	all := meta.GetAll("multiple")
	if len(all) != 3 {
		t.Errorf("GetAll multiple: got %d values, want 3", len(all))
	}

	if !meta.Has("empty") {
		t.Error("Has empty: should be true")
	}

	if meta.Has("missing") {
		t.Error("Has missing: should be false")
	}
}
