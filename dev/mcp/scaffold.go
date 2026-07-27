package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScaffoldResult reports what was written and what to run next.
type ScaffoldResult struct {
	Directory string   `json:"directory"`
	Module    string   `json:"module"`
	Files     []string `json:"files"`
	Next      []string `json:"next_actions"`
	Notes     []string `json:"notes"`
	// Verification is the standing of the directory right after scaffolding:
	// never verified, because nothing has been run yet.
	Verification *Status `json:"verification,omitempty"`
}

// Scaffold writes a Foundation application that compiles, passes the analyzer,
// and has a test. It exists so the first file of a project is not improvised:
// every construct here is the one the analyzer and the generator expect.
func Scaffold(ctx context.Context, dir, module string, withServer bool) (*ScaffoldResult, error) {
	module = strings.TrimSpace(module)
	if module == "" {
		return nil, fmt.Errorf("mcp: a module path is required, for example example.com/service")
	}
	target, err := filepath.Abs(strings.TrimSpace(orDefault(dir, ".")))
	if err != nil {
		return nil, fmt.Errorf("mcp: resolve %q: %w", dir, err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, fmt.Errorf("mcp: create %s: %w", target, err)
	}

	runtimeVersion := "latest"
	if loaded, err := API(); err == nil && loaded.Module != "" {
		runtimeVersion = "latest"
	}

	files := map[string]string{
		"go.mod":      scaffoldGoMod(module),
		"users.go":    scaffoldDomain(),
		"handlers.go": scaffoldHandlers(),
		"app.go":      scaffoldApp(withServer),
		"app_test.go": scaffoldTest(),
		"README.md":   scaffoldReadme(module),
		".gitignore":  "/dist/\n",
	}

	existing := make([]string, 0, len(files))
	for name := range files {
		if _, err := os.Stat(filepath.Join(target, name)); err == nil {
			existing = append(existing, name)
		}
	}
	if len(existing) > 0 {
		sort.Strings(existing)
		return nil, fmt.Errorf(
			"mcp: refusing to overwrite existing files in %s: %s",
			target, strings.Join(existing, ", "),
		)
	}

	written := make([]string, 0, len(files))
	for name, content := range files {
		path := filepath.Join(target, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("mcp: write %s: %w", path, err)
		}
		written = append(written, name)
	}
	sort.Strings(written)

	return &ScaffoldResult{
		Directory: target,
		Module:    module,
		Files:     written,
		Next: []string{
			fmt.Sprintf("go get github.com/mirkobrombin/go-foundation/v2@%s", runtimeVersion),
			"foundation generate ./...",
			"foundation check ./...",
			"go test ./...",
			"Then call foundation_verify on this directory before reporting the work as done.",
		},
		Notes: []string{
			"RegisterFoundation is produced by foundation generate. The project does not compile until you run it, and that is deliberate.",
			"zz_foundation.gen.go is meant to be committed.",
			"The handler and the action here are the canonical shapes: copy them rather than inventing tags.",
		},
	}, nil
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func scaffoldGoMod(module string) string {
	return fmt.Sprintf(`module %s

go 1.25
`, module)
}

func scaffoldDomain() string {
	return `package main

import (
	"sync"

	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

// User is the model the service exposes.
type User struct {
	ID   int    ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

// UserStore is the contract the handlers depend on.
type UserStore interface {
	Create(name string) User
	Find(id int) (User, bool)
}

// MemoryUserStore keeps users in memory. The embedded marker records that it
// implements UserStore; foundation generate turns it into a compile-time
// assertion.
type MemoryUserStore struct {
	contracts.Implements[UserStore]

	mu    sync.RWMutex
	users map[int]User
	next  int
}

// NewMemoryUserStore returns an empty store.
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{users: make(map[int]User), next: 1}
}

// Create stores a user and returns it.
func (s *MemoryUserStore) Create(name string) User {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := User{ID: s.next, Name: name}
	s.users[user.ID] = user
	s.next++
	return user
}

// Find returns a user by identifier.
func (s *MemoryUserStore) Find(id int) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	return user, ok
}
`
}

func scaffoldHandlers() string {
	return `package main

import (
	"context"
	"fmt"
)

// GetUser answers GET /users/{id}. The route lives in the blank field, the
// parameter is bound by the path tag, and the dependency by the inject tag.
type GetUser struct {
	_     struct{}  ` + "`method:\"GET\" path:\"/users/{id:int}\"`" + `
	ID    int       ` + "`path:\"id\"`" + `
	Users UserStore ` + "`inject:\"users\"`" + `
}

// Handle returns the requested user.
func (h *GetUser) Handle(context.Context) (any, error) {
	user, ok := h.Users.Find(h.ID)
	if !ok {
		return nil, fmt.Errorf("user %d not found", h.ID)
	}
	return user, nil
}

// CreateUser is an action, dispatched by name rather than by route.
type CreateUser struct {
	_     struct{}  ` + "`action:\"users.create\" keys:\"ctrl+n\"`" + `
	Name  string    ` + "`json:\"name\"`" + `
	Users UserStore ` + "`inject:\"users\"`" + `
}

// Handle creates a user.
func (a *CreateUser) Handle(context.Context) (any, error) {
	if a.Name == "" {
		return nil, fmt.Errorf("user name is required")
	}
	return a.Users.Create(a.Name), nil
}
`
}

func scaffoldApp(withServer bool) string {
	body := `package main

import (
	"log"

	"github.com/mirkobrombin/go-foundation/v2/app"
)

// Build wires the application. Every dependency is provided by name here, and
// RegisterFoundation comes from the generated registry.
func Build() (*app.App, error) {
	application := app.New().
		Provide("users", NewMemoryUserStore())
	RegisterFoundation(application)
	if _, err := application.Build(); err != nil {
		return nil, err
	}
	return application, nil
}
`
	if !withServer {
		return body + `
func main() {
	if _, err := Build(); err != nil {
		log.Fatal(err)
	}
	log.Println("application built")
}
`
	}
	return body + `
func main() {
	application, err := Build()
	if err != nil {
		log.Fatal(err)
	}
	// An empty address binds to 127.0.0.1:8080. Pass a public address only when
	// the service is meant to accept remote traffic.
	if err := application.Listen(""); err != nil {
		log.Fatal(err)
	}
}
`
}

func scaffoldTest() string {
	return `package main

import (
	"context"
	"testing"
)

func TestBuild(t *testing.T) {
	if _, err := Build(); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
}

func TestCreateUserAction(t *testing.T) {
	application, err := Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	result, err := application.Dispatch(
		context.Background(),
		"users.create",
		map[string]any{"Name": "Ada"},
	)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	user, ok := result.(User)
	if !ok {
		t.Fatalf("Dispatch() result type = %T", result)
	}
	if user.Name != "Ada" {
		t.Fatalf("Dispatch() result = %#v", user)
	}
}
`
}

func scaffoldReadme(module string) string {
	return fmt.Sprintf(`# %s

A Foundation service.

## Working on it

    foundation generate ./...
    foundation check ./...
    go test ./...

The generated registry, zz_foundation.gen.go, is committed on purpose: it turns
a broken contract into a compile error on any machine, with or without the CLI.
`, module)
}
