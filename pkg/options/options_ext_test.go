package options

import (
	"testing"
)

func TestMergeConfig_LastWins(t *testing.T) {
	type Config struct {
		Debug bool
		Port  int
	}

	defaults := Config{Debug: true, Port: 8080}
	override := Config{Debug: false, Port: 0}

	result := MergeConfig(LastWins, defaults, override)
	if result.Debug != false {
		t.Errorf("Debug: got %v, want false", result.Debug)
	}
	if result.Port != 0 {
		t.Errorf("Port: got %d, want 0", result.Port)
	}
}

func TestMergeConfig_SkipZero(t *testing.T) {
	type Config struct {
		Port  int
		Host  string
	}

	defaults := Config{Port: 8080, Host: "localhost"}
	override := Config{Port: 0, Host: "overridden"}

	result := MergeConfig(SkipZero, defaults, override)
	if result.Port != 8080 {
		t.Errorf("Port: got %d, want 8080 (zero should not overwrite)", result.Port)
	}
	if result.Host != "overridden" {
		t.Errorf("Host: got %q, want %q", result.Host, "overridden")
	}
}

func TestConfigure(t *testing.T) {
	type AppOptions struct {
		Host string `config:"host" required:"true"`
		Port int    `config:"port"`
	}

	opts := Configure(AppOptions{Host: "localhost", Port: 8080})
	if opts.Value.Host != "localhost" {
		t.Errorf("Configure: got %q, want %q", opts.Value.Host, "localhost")
	}
}

func TestMustValidate(t *testing.T) {
	type Config struct {
		Name string `required:"true"`
		Port int
	}

	t.Run("valid", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("should not panic: %v", r)
			}
		}()
		MustValidate(Config{Name: "app"})
	})

	t.Run("missing required", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("should panic for missing required field")
			}
		}()
		MustValidate(Config{Name: ""})
	})
}