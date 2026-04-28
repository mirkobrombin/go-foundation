package reflectutil

import (
	"reflect"
	"testing"
	"time"
)

func TestBind(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		var s string
		err := Bind(reflect.ValueOf(&s).Elem(), "hello")
		if err != nil || s != "hello" {
			t.Errorf("Bind failed: %v, s=%q", err, s)
		}
	})

	t.Run("Int", func(t *testing.T) {
		var i int
		err := Bind(reflect.ValueOf(&i).Elem(), "42")
		if err != nil || i != 42 {
			t.Errorf("Bind failed: %v, i=%d", err, i)
		}
	})

	t.Run("Duration", func(t *testing.T) {
		var d time.Duration
		err := Bind(reflect.ValueOf(&d).Elem(), "5s")
		if err != nil || d != 5*time.Second {
			t.Errorf("Bind failed: %v, d=%v", err, d)
		}
	})

	t.Run("Bool", func(t *testing.T) {
		var b bool
		err := Bind(reflect.ValueOf(&b).Elem(), "yes")
		if err != nil || !b {
			t.Errorf("Bind failed: %v, b=%v", err, b)
		}
	})

	t.Run("Float", func(t *testing.T) {
		var f float64
		err := Bind(reflect.ValueOf(&f).Elem(), "3.14")
		if err != nil || f != 3.14 {
			t.Errorf("Bind failed: %v, f=%v", err, f)
		}
	})

	t.Run("Slice", func(t *testing.T) {
		var s []string
		err := Bind(reflect.ValueOf(&s).Elem(), "a")
		if err != nil || len(s) != 1 || s[0] != "a" {
			t.Errorf("Bind failed: %v, s=%v", err, s)
		}
	})
}

func TestParseBoolExtended(t *testing.T) {
	tests := []struct {
		input string
		want  bool
		err   bool
	}{
		{"true", true, false},
		{"yes", true, false},
		{"on", true, false},
		{"1", true, false},
		{"false", false, false},
		{"no", false, false},
		{"off", false, false},
		{"0", false, false},
		{"invalid", false, true},
	}

	for _, tt := range tests {
		got, err := ParseBoolExtended(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseBoolExtended(%q) error = %v, wantErr %v", tt.input, err, tt.err)
		}
		if got != tt.want {
			t.Errorf("ParseBoolExtended(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}