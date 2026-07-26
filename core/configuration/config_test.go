package configuration

import (
	"context"
	"testing"
)

type staticProvider map[string]any

func (p staticProvider) Name() string { return "static" }

func (p staticProvider) Load(ctx context.Context) (map[string]any, error) {
	return map[string]any(p), nil
}

func TestConfigurationGetAndSection(t *testing.T) {
	cfg, err := NewBuilder().
		Add(staticProvider{"db:host": "localhost", "db:port": "5432", "feature": "true"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got, ok := cfg.GetString("DB:HOST"); !ok || got != "localhost" {
		t.Fatalf("GetString() = %q, %v", got, ok)
	}
	if got, ok := cfg.GetInt("db:port"); !ok || got != 5432 {
		t.Fatalf("GetInt() = %d, %v", got, ok)
	}
	if got, ok := cfg.GetBool("feature"); !ok || !got {
		t.Fatalf("GetBool() = %v, %v", got, ok)
	}

	section := cfg.GetSection("db")
	if got, ok := section.GetString("host"); !ok || got != "localhost" {
		t.Fatalf("section GetString() = %q, %v", got, ok)
	}
}

func TestConfigurationBind(t *testing.T) {
	cfg, err := NewBuilder().
		Add(staticProvider{"db:host": "localhost", "port": "8080"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	var out struct {
		Host    string `conf:"db:host"`
		Port    int
		Enabled bool `conf:"enabled" default:"true"`
	}
	if err := cfg.Bind(&out); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if out.Host != "localhost" || out.Port != 8080 || !out.Enabled {
		t.Fatalf("Bind() = %+v", out)
	}
}

func TestConfigurationBindRejectsNonStruct(t *testing.T) {
	if err := New().Bind("bad"); err == nil {
		t.Fatal("Bind() error = nil, want error")
	}
}
