package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := sampleModule(t, true)
	outputs, err := Load(context.Background(), dir, "./...")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("Load() returned %d outputs", len(outputs))
	}

	source := string(outputs[0].Content)
	for _, expected := range []string{
		"var _ Store = (*MemoryStore)(nil)",
		"func FoundationHTTPHandlers()",
		`Method: "GET", Path: "/items/{id:int}"`,
		"func FoundationActions()",
		`Name: "file.save", Key: "ctrl+s"`,
		"func RegisterFoundation(",
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("generated source does not contain %q\n%s", expected, source)
		}
	}
}

func TestWriteAndCheck(t *testing.T) {
	dir := sampleModule(t, true)
	paths, err := Write(context.Background(), dir, "./...")
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("Write() returned %d paths", len(paths))
	}
	if err := Check(context.Background(), dir, "./..."); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestWriteRemovesOrphanedGeneratedFile(t *testing.T) {
	dir := sampleModule(t, true)
	paths, err := Write(context.Background(), dir, "./...")
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("Write() returned %d paths", len(paths))
	}

	source := `package sample

func Register() {}
`
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), dir, "./..."); err == nil {
		t.Fatal("Check() accepted an orphaned generated file")
	}
	if _, err := Write(context.Background(), dir, "./..."); err != nil {
		t.Fatalf("Write() cleanup error = %v", err)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("generated file still exists: %v", err)
	}
}

func TestWriteBootstrapsGeneratedRegistration(t *testing.T) {
	dir := sampleModule(t, true)
	path := filepath.Join(dir, "use_generated.go")
	source := `package sample

func UseGenerated() {
	RegisterFoundation(nil)
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(context.Background(), dir, "./..."); err != nil {
		t.Fatalf("Write() bootstrap error = %v", err)
	}
}

func TestWriteRejectsUnknownGeneratedRegistration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/empty\n\ngo 1.25\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	source := `package empty

func UseGenerated() {
	RegisterFoundation(nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "empty.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(context.Background(), dir, "./..."); err == nil {
		t.Fatal("Write() accepted an unknown generated registration")
	}
}

func TestWriteDoesNotOverwriteUserFile(t *testing.T) {
	dir := sampleModule(t, true)
	path := filepath.Join(dir, generatedFile)
	content := []byte("package sample\n\nvar Keep = true\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(context.Background(), dir, "./..."); err == nil {
		t.Fatal("Write() overwrote a user-owned generated filename")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(content) {
		t.Fatal("Write() changed the user-owned file")
	}
}

func TestLoadRejectsPackageOutsideModule(t *testing.T) {
	dir := sampleModule(t, true)
	if _, err := Load(context.Background(), dir, "fmt"); err == nil {
		t.Fatal("Load() accepted a package outside the module")
	}
}

func TestLoadRejectsInvalidContract(t *testing.T) {
	dir := sampleModule(t, false)
	if _, err := Load(context.Background(), dir, "./..."); err == nil {
		t.Fatal("Load() accepted an invalid contract")
	}
}

func TestLoadRejectsInvalidHandler(t *testing.T) {
	dir := sampleModule(t, true)
	source := `package sample

type BrokenHandler struct {
	_ struct{} ` + "`" + `action:"broken"` + "`" + `
}
`
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), dir, "./..."); err == nil {
		t.Fatal("Load() accepted an invalid handler")
	}
}

func TestLoadRejectsInvalidRoute(t *testing.T) {
	dir := sampleModule(t, true)
	source := `package sample

import "context"

type BrokenRoute struct {
	_ struct{} ` + "`" + `method:"GET" path:"/files/{*path:int}"` + "`" + `
}

func (h *BrokenRoute) Handle(context.Context) (any, error) {
	return nil, nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "broken_route.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), dir, "./..."); err == nil {
		t.Fatal("Load() accepted an invalid route")
	}
}

func TestLoadRejectsConflictingParameterBranches(t *testing.T) {
	dir := sampleModule(t, true)
	source := `package sample

import "context"

type ConflictingRoute struct {
	_    struct{} ` + "`" + `method:"POST" path:"/items/{*path}"` + "`" + `
	Path string ` + "`" + `path:"path"` + "`" + `
}

func (h *ConflictingRoute) Handle(context.Context) (any, error) {
	return nil, nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "conflicting_route.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), dir, "./..."); err == nil {
		t.Fatal("Load() accepted conflicting parameter branches")
	}
}

func sampleModule(t *testing.T, valid bool) string {
	t.Helper()
	dir := t.TempDir()
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	goMod := fmt.Sprintf(`module example.com/sample

go 1.25.0

require github.com/mirkobrombin/go-foundation/v2 v2.0.0

replace github.com/mirkobrombin/go-foundation/v2 => %s
`, root)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	method := ""
	if valid {
		method = `
func (s *MemoryStore) Get() string {
	return "value"
}
`
	}
	source := `package sample

import (
	"context"

	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

type Store interface {
	Get() string
}

type MemoryStore struct {
	contracts.Implements[Store]
}
` + method + `
type GetItem struct {
	_  struct{} ` + "`" + `method:"GET" path:"/items/{id:int}"` + "`" + `
	ID int      ` + "`" + `path:"id"` + "`" + `
}

func (h *GetItem) Handle(context.Context) (any, error) {
	return h.ID, nil
}

type Save struct {
	_ struct{} ` + "`" + `action:"file.save" keys:"ctrl+s"` + "`" + `
}

func (h *Save) Handle(context.Context) (any, error) {
	return nil, nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
