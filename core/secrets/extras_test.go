package secrets

import (
	"bytes"
	"errors"
	"testing"
)

func TestCipherStoreBindsCiphertextToKey(t *testing.T) {
	raw := NewMemoryStore()
	store, err := NewCipherStore(raw, bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("one", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("two", []byte("second")); err != nil {
		t.Fatal(err)
	}
	second, err := raw.Get("two")
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Set("one", second); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("one"); err == nil {
		t.Fatal("Get() accepted ciphertext copied from another key")
	}
}

func TestCipherStoreClonesEncryptionKey(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	store, err := NewCipherStore(NewMemoryStore(), key)
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 2
	if err := store.Set("key", []byte("value")); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get("key")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "value" {
		t.Fatalf("Get() = %q", value)
	}
}

type failingStore struct {
	err error
}

func (s failingStore) Set(string, []byte) error   { return s.err }
func (s failingStore) Get(string) ([]byte, error) { return nil, s.err }
func (s failingStore) Delete(string) error        { return s.err }

func TestFallbackStoreDoesNotHidePrimaryFailures(t *testing.T) {
	primaryErr := errors.New("authentication failed")
	secondary := NewMemoryStore()
	_ = secondary.Set("key", []byte("stale"))
	store := NewFallbackStore(failingStore{err: primaryErr}, secondary)

	if _, err := store.Get("key"); !errors.Is(err, primaryErr) {
		t.Fatalf("Get() error = %v, want primary error", err)
	}
	if err := store.Set("key", []byte("secret")); !errors.Is(err, primaryErr) {
		t.Fatalf("Set() error = %v, want primary error", err)
	}
}

func TestFallbackStoreUsesSecondaryForExpectedConditions(t *testing.T) {
	secondary := NewMemoryStore()
	store := NewFallbackStore(failingStore{err: ErrNotFound}, secondary)
	_ = secondary.Set("key", []byte("value"))
	value, err := store.Get("key")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "value" {
		t.Fatalf("Get() = %q", value)
	}

	store = NewFallbackStore(failingStore{err: ErrReadOnly}, secondary)
	if err := store.Set("other", []byte("stored")); err != nil {
		t.Fatal(err)
	}
}
