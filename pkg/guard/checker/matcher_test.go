package checker

import (
	"fmt"
	"reflect"
	"testing"
)

type stringID string

func (s stringID) String() string {
	return string(s)
}

func TestIsMatchScalarValues(t *testing.T) {
	tests := []struct {
		name string
		val  any
		id   string
		want bool
	}{
		{"string", "42", "42", true},
		{"int", int64(42), "42", true},
		{"uint", uint(42), "42", true},
		{"stringer", stringID("42"), "42", true},
		{"fallback", fmt.Errorf("42"), "42", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMatch(reflect.ValueOf(tt.val), tt.id); got != tt.want {
				t.Fatalf("IsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMatchCollectionsAndPointers(t *testing.T) {
	value := "42"
	tests := []struct {
		name string
		val  any
		want bool
	}{
		{"slice", []string{"1", "42"}, true},
		{"array", [2]int{1, 42}, true},
		{"map key", map[string]string{"42": "owner"}, true},
		{"pointer", &value, true},
		{"missing", []string{"1", "2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMatch(reflect.ValueOf(tt.val), "42"); got != tt.want {
				t.Fatalf("IsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMatchNilPointer(t *testing.T) {
	var value *string
	if IsMatch(reflect.ValueOf(value), "42") {
		t.Fatal("IsMatch() = true, want false")
	}
}
