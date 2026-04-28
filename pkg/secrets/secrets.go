package secrets

import (
	"errors"
	"os"
	"sync"
)

var (
	// ErrNotFound indicates that a secret key does not exist.
	ErrNotFound = errors.New("secrets: not found")
	// ErrReadOnly indicates that the selected store does not support writes.
	ErrReadOnly = errors.New("secrets: read-only store")
	// ErrNotImplemented indicates that the requested backend is only a placeholder.
	ErrNotImplemented = errors.New("secrets: not implemented")
)

// Store is the shared contract for secret backends.
type Store interface {
	Set(key string, value []byte) error
	Get(key string) ([]byte, error)
	Delete(key string) error
}

// MemoryStore is a thread-safe in-memory store intended for tests and ephemeral use.
type MemoryStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: make(map[string][]byte)}
}

// Set stores a copy of the provided secret value.
func (s *MemoryStore) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), value...)
	return nil
}

// Get returns a copy of the stored secret value.
func (s *MemoryStore) Get(key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.m[key]
	if !ok {
		return nil, ErrNotFound
	}

	return append([]byte(nil), v...), nil
}

// Delete removes a secret from the in-memory store.
func (s *MemoryStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// EnvStore provides read-only access to environment variables.
type EnvStore struct{}

// NewEnvStore creates a new environment-backed secret store.
func NewEnvStore() *EnvStore {
	return &EnvStore{}
}

// Set reports that environment-backed stores are read-only.
func (e *EnvStore) Set(key string, value []byte) error {
	return ErrReadOnly
}

// Get returns the environment variable value for the provided key.
func (e *EnvStore) Get(key string) ([]byte, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil, ErrNotFound
	}
	return []byte(v), nil
}

// Delete reports that environment-backed stores are read-only.
func (e *EnvStore) Delete(key string) error {
	return ErrReadOnly
}

// VaultStore is a placeholder for an external secret manager implementation.
type VaultStore struct{}

// NewVaultStore creates a placeholder Vault-backed store.
func NewVaultStore() *VaultStore {
	return &VaultStore{}
}

// Set reports that the placeholder Vault store is not implemented yet.
func (v *VaultStore) Set(key string, value []byte) error {
	return ErrNotImplemented
}

// Get reports that the placeholder Vault store is not implemented yet.
func (v *VaultStore) Get(key string) ([]byte, error) {
	return nil, ErrNotImplemented
}

// Delete reports that the placeholder Vault store is not implemented yet.
func (v *VaultStore) Delete(key string) error {
	return ErrNotImplemented
}
