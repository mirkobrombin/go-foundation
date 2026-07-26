package secrets_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mirkobrombin/go-foundation/v2/core/secrets"
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

func TestVaultStoreRequiresConfig(t *testing.T) {
	store := secrets.NewVaultStore()
	if err := store.Set("k", []byte("v")); !errors.Is(err, secrets.ErrNotConfigured) {
		t.Fatalf("Set() error = %v, want ErrNotConfigured", err)
	}
}

func TestVaultStoreRoundTrip(t *testing.T) {
	values := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "token" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/secret/data/app/db" {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}

		switch r.Method {
		case http.MethodPut:
			var in struct {
				Data map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			values["app/db"] = in.Data["value"]
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"data": map[string]string{"value": values["app/db"]},
				},
			})
		case http.MethodDelete:
			delete(values, "app/db")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	store := secrets.NewVaultStore(
		secrets.WithVaultAddress(server.URL),
		secrets.WithVaultToken("token"),
	)

	if err := store.Set("app/db", []byte("password")); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if values["app/db"] != base64.StdEncoding.EncodeToString([]byte("password")) {
		t.Fatalf("stored value = %q", values["app/db"])
	}

	got, err := store.Get("app/db")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "password" {
		t.Fatalf("Get() = %q, want password", string(got))
	}

	if err := store.Delete("app/db"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := values["app/db"]; ok {
		t.Fatal("Delete() did not remove value")
	}
}

func TestVaultStoreGetMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	store := secrets.NewVaultStore(
		secrets.WithVaultAddress(server.URL),
		secrets.WithVaultToken("token"),
	)

	_, err := store.Get("missing")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestVaultStoreRejectsInvalidKeys(t *testing.T) {
	store := secrets.NewVaultStore(
		secrets.WithVaultAddress("https://vault.example.com"),
		secrets.WithVaultToken("token"),
	)

	tests := []string{"", ".", "..", "app/../db", "app//db"}
	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			if err := store.Set(key, []byte("value")); !errors.Is(err, secrets.ErrInvalidKey) {
				t.Fatalf("Set() error = %v, want ErrInvalidKey", err)
			}
		})
	}
}

func TestVaultStoreRejectsPlainHTTPRemoteAddress(t *testing.T) {
	store := secrets.NewVaultStore(
		secrets.WithVaultAddress("http://vault.example.com"),
		secrets.WithVaultToken("token"),
	)

	if err := store.Set("key", []byte("value")); !errors.Is(err, secrets.ErrInsecureVaultAddress) {
		t.Fatalf("Set() error = %v, want ErrInsecureVaultAddress", err)
	}
}

func TestVaultStoreDoesNotFollowRedirects(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	store := secrets.NewVaultStore(
		secrets.WithVaultAddress(server.URL),
		secrets.WithVaultToken("token"),
	)

	if err := store.Set("key", []byte("value")); err == nil {
		t.Fatal("Set() error = nil, want redirect status error")
	}
	if hits != 1 {
		t.Fatalf("redirect hits = %d, want 1", hits)
	}
}

func TestVaultStoreCustomClientDoesNotFollowRedirects(t *testing.T) {
	destinationHits := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationHits++
		if r.Header.Get("X-Vault-Token") != "" {
			t.Error("redirect leaked Vault token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	store := secrets.NewVaultStore(
		secrets.WithVaultAddress(source.URL),
		secrets.WithVaultToken("token"),
		secrets.WithVaultClient(&http.Client{}),
	)
	if err := store.Set("key", []byte("value")); err == nil {
		t.Fatal("Set() followed a redirect with a custom client")
	}
	if destinationHits != 0 {
		t.Fatalf("redirect destination hits = %d, want 0", destinationHits)
	}
}

func TestVaultStoreRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, (1<<20)+1))
	}))
	defer server.Close()

	store := secrets.NewVaultStore(
		secrets.WithVaultAddress(server.URL),
		secrets.WithVaultToken("token"),
	)
	if _, err := store.Get("key"); err == nil {
		t.Fatal("Get() accepted an oversized Vault response")
	}
}
