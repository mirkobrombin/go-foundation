package env

import (
	"context"
	"testing"
)

func TestProviderLoadWithPrefix(t *testing.T) {
	t.Setenv("APP_HOST", "localhost")
	t.Setenv("OTHER_HOST", "remote")

	data, err := New("APP_").Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if data["host"] != "localhost" {
		t.Fatalf("host = %v, want localhost", data["host"])
	}
	if _, ok := data["other_host"]; ok {
		t.Fatal("Load() included key outside prefix")
	}
}

func TestProviderLoadWithoutPrefix(t *testing.T) {
	t.Setenv("GO_FOUNDATION_ENV_TEST", "yes")

	data, err := New("").Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if data["go_foundation_env_test"] != "yes" {
		t.Fatalf("go_foundation_env_test = %v, want yes", data["go_foundation_env_test"])
	}
}
