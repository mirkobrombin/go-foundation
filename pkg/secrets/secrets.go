package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

var (
	// ErrNotFound indicates that a secret key does not exist.
	ErrNotFound = errors.New("secrets: not found")
	// ErrReadOnly indicates that the selected store does not support writes.
	ErrReadOnly = errors.New("secrets: read-only store")
	// ErrNotConfigured indicates that the selected store is missing required settings.
	ErrNotConfigured = errors.New("secrets: not configured")
	// ErrInvalidKey indicates that a secret key cannot be represented safely.
	ErrInvalidKey = errors.New("secrets: invalid key")
	// ErrInsecureVaultAddress indicates that a Vault address would send secrets over an unsafe channel.
	ErrInsecureVaultAddress = errors.New("secrets: vault address must use https")
	// ErrNotImplemented is kept for compatibility with older releases.
	// Deprecated: VaultStore now returns ErrNotConfigured when setup is incomplete.
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

// VaultStore stores secrets in a HashiCorp Vault KV v2 mount.
type VaultStore struct {
	addr   string
	token  string
	mount  string
	client *http.Client
}

// VaultOption configures a VaultStore.
type VaultOption func(*VaultStore)

// NewVaultStore creates a Vault-backed store.
func NewVaultStore(opts ...VaultOption) *VaultStore {
	store := &VaultStore{
		addr:   os.Getenv("VAULT_ADDR"),
		token:  os.Getenv("VAULT_TOKEN"),
		mount:  "secret",
		client: defaultVaultClient(),
	}
	for _, opt := range opts {
		opt(store)
	}
	return store
}

// WithVaultAddress sets the Vault base URL.
func WithVaultAddress(addr string) VaultOption {
	return func(v *VaultStore) { v.addr = strings.TrimRight(addr, "/") }
}

// WithVaultToken sets the Vault token.
func WithVaultToken(token string) VaultOption {
	return func(v *VaultStore) { v.token = token }
}

// WithVaultMount sets the KV v2 mount name.
func WithVaultMount(mount string) VaultOption {
	return func(v *VaultStore) { v.mount = strings.Trim(mount, "/") }
}

// WithVaultClient sets the HTTP client.
func WithVaultClient(client *http.Client) VaultOption {
	return func(v *VaultStore) {
		if client != nil {
			v.client = client
		}
	}
}

// Set stores a secret value in Vault.
func (v *VaultStore) Set(key string, value []byte) error {
	if err := v.ready(); err != nil {
		return err
	}
	body := map[string]any{
		"data": map[string]string{
			"value": base64.StdEncoding.EncodeToString(value),
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := v.request(http.MethodPut, key, bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return vaultStatus(resp, http.StatusOK, http.StatusNoContent)
}

// Get reads a secret value from Vault.
func (v *VaultStore) Get(key string) ([]byte, error) {
	if err := v.ready(); err != nil {
		return nil, err
	}
	req, err := v.request(http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if err := vaultStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}

	var out struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	encoded, ok := out.Data.Data["value"]
	if !ok {
		return nil, ErrNotFound
	}
	value, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secrets: vault decode failed: %w", err)
	}
	return value, nil
}

// Delete removes a secret from Vault.
func (v *VaultStore) Delete(key string) error {
	if err := v.ready(); err != nil {
		return err
	}
	req, err := v.request(http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return vaultStatus(resp, http.StatusOK, http.StatusNoContent, http.StatusNotFound)
}

func (v *VaultStore) ready() error {
	if v.addr == "" || v.token == "" || v.mount == "" || v.client == nil {
		return ErrNotConfigured
	}
	parsed, err := url.Parse(v.addr)
	if err != nil || parsed.Host == "" {
		return ErrNotConfigured
	}
	if parsed.Scheme != "https" && !isLoopbackHTTP(parsed) {
		return ErrInsecureVaultAddress
	}
	return nil
}

func (v *VaultStore) request(method, key string, body io.Reader) (*http.Request, error) {
	escapedKey, err := escapeVaultKey(key)
	if err != nil {
		return nil, err
	}
	u := strings.TrimRight(v.addr, "/") + "/v1/" + url.PathEscape(v.mount) + "/data/" + escapedKey
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", v.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func escapeVaultKey(key string) (string, error) {
	parts := strings.Split(strings.Trim(key, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ErrInvalidKey
	}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ErrInvalidKey
		}
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/"), nil
}

func vaultStatus(resp *http.Response, allowed ...int) error {
	for _, code := range allowed {
		if resp.StatusCode == code {
			return nil
		}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("secrets: vault status %d: %s", resp.StatusCode, string(body))
}

func isLoopbackHTTP(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func defaultVaultClient() *http.Client {
	client := *http.DefaultClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}
