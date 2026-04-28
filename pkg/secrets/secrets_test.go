package secrets_test

import (
	"errors"
	"os"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/secrets"
)

func TestMemoryStoreCopiesValues(t *testing.T) {
	store := secrets.NewMemoryStore()
	value := []byte("value")

	if err := store.Set("k", value); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	value[0] = 'X'

	got, err := store.Get("k")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "value" {
		t.Fatalf("Get() value = %q, want %q", string(got), "value")
	}

	got[0] = 'Y'
	gotAgain, err := store.Get("k")
	if err != nil {
		t.Fatalf("Get() second error = %v", err)
	}
	if string(gotAgain) != "value" {
		t.Fatalf("Get() second value = %q, want %q", string(gotAgain), "value")
	}
}

func TestMemoryStoreDeleteRemovesValue(t *testing.T) {
	store := secrets.NewMemoryStore()
	_ = store.Set("k", []byte("v"))

	if err := store.Delete("k"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get("k"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestEnvStoreIsReadOnly(t *testing.T) {
	t.Setenv("GO_SECRETS_TOKEN", "token-value")
	store := secrets.NewEnvStore()

	value, err := store.Get("GO_SECRETS_TOKEN")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(value) != "token-value" {
		t.Fatalf("Get() value = %q, want %q", string(value), "token-value")
	}

	if err := store.Set("GO_SECRETS_TOKEN", []byte("x")); !errors.Is(err, secrets.ErrReadOnly) {
		t.Fatalf("Set() error = %v, want ErrReadOnly", err)
	}
	if err := store.Delete("GO_SECRETS_TOKEN"); !errors.Is(err, secrets.ErrReadOnly) {
		t.Fatalf("Delete() error = %v, want ErrReadOnly", err)
	}
}

func TestEnvStoreReturnsNotFound(t *testing.T) {
	_ = os.Unsetenv("GO_SECRETS_MISSING")
	_, err := secrets.NewEnvStore().Get("GO_SECRETS_MISSING")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestVaultStoreReturnsNotImplemented(t *testing.T) {
	store := secrets.NewVaultStore()
	if err := store.Set("k", []byte("v")); !errors.Is(err, secrets.ErrNotImplemented) {
		t.Fatalf("Set() error = %v, want ErrNotImplemented", err)
	}
}
