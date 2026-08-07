package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeModule(t *testing.T, requires string) string {
	t.Helper()

	dir := t.TempDir()
	manifest := "module example.com/sample\n\ngo 1.25\n"
	if requires != "" {
		manifest += "\nrequire " + requires + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestOfflineScanIsNotAPass is the property a build gate depends on. An offline
// scan finds nothing because it looked nowhere, and reporting that as success
// would be worse than having no gate at all.
func TestOfflineScanIsNotAPass(t *testing.T) {
	result, err := Run(context.Background(), Options{
		Directory: writeModule(t, "golang.org/x/sys v0.41.0"),
		Offline:   true,
		SkipSAST:  true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Dependencies != 1 {
		t.Fatalf("dependencies = %d, want 1", result.Dependencies)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("offline scan reported findings: %+v", result.Findings)
	}
	if len(result.Degraded) == 0 {
		t.Fatal("an offline scan must record that it is incomplete")
	}
	if result.Passed {
		t.Fatal("an incomplete scan must not pass")
	}
	if !strings.Contains(result.Summary, "not an all clear") {
		t.Fatalf("summary = %q, it must not read as a pass", result.Summary)
	}
}

func TestScanCapturesTheScannerOutput(t *testing.T) {
	result, err := Run(context.Background(), Options{
		Directory: writeModule(t, "golang.org/x/sys v0.41.0"),
		Offline:   true,
		SkipSAST:  true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output == "" {
		t.Fatal("the scanner output was neither captured nor surfaced")
	}
	if !strings.Contains(result.Output, "go.mod") {
		t.Fatalf("captured output does not mention the manifest read: %q", result.Output)
	}
}

func TestScanWritesTheSBOMWhenAsked(t *testing.T) {
	target := filepath.Join(t.TempDir(), "sbom.json")
	result, err := Run(context.Background(), Options{
		Directory: writeModule(t, "golang.org/x/sys v0.41.0"),
		Name:      "sample",
		Version:   "1.0.0",
		Offline:   true,
		SkipSAST:  true,
		SBOMPath:  target,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.SBOMPath != target {
		t.Fatalf("SBOMPath = %q, want %q", result.SBOMPath, target)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the SBOM was not written: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("the SBOM is not valid JSON: %v", err)
	}
	components, _ := document["components"].([]any)
	if len(components) != 1 {
		t.Fatalf("components = %d, want 1", len(components))
	}
}

func TestScanRejectsAnUnusableDirectory(t *testing.T) {
	if _, err := Run(context.Background(), Options{
		Directory: filepath.Join(t.TempDir(), "absent"),
		Offline:   true,
	}); err == nil {
		t.Fatal("a missing directory must be an error, not an empty scan")
	}
}

func TestReportShowsFindingsAndGaps(t *testing.T) {
	var builder strings.Builder
	Report(&builder, &Result{
		Summary:  "1 finding(s)",
		Findings: []Finding{{Kind: "vulnerability", ID: "CVE-1", Severity: "high", Subject: "example.com/dep"}},
		Degraded: []string{"databases unreachable"},
	})

	rendered := builder.String()
	for _, want := range []string{"CVE-1", "example.com/dep", "incomplete", "databases unreachable"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the report omits %q: %s", want, rendered)
		}
	}
}
