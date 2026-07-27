package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mirkobrombin/go-foundation/dev/v2/analyzer"
	"github.com/mirkobrombin/go-foundation/dev/v2/generator"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

const commandTimeout = 10 * time.Minute

// Diagnostic is one analyzer finding, in a form a caller can act on without
// parsing text.
type Diagnostic struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Step is the outcome of one verification command.
type Step struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Passed  bool   `json:"passed"`
	Output  string `json:"output,omitempty"`
}

// CheckResult is the analyzer outcome for a workspace.
type CheckResult struct {
	Directory    string       `json:"directory"`
	Patterns     []string     `json:"patterns"`
	Passed       bool         `json:"passed"`
	Diagnostics  []Diagnostic `json:"diagnostics"`
	Summary      string       `json:"summary"`
	Verification *Status      `json:"verification,omitempty"`
}

// VerifyResult is the full verification of a workspace.
type VerifyResult struct {
	Directory   string       `json:"directory"`
	Passed      bool         `json:"passed"`
	Steps       []Step       `json:"steps"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Summary     string       `json:"summary"`
	NextActions []string     `json:"next_actions,omitempty"`
	// Receipt is issued only when everything passed, and only for the code as it
	// stood during the run. Reporting success without quoting it is a claim these
	// tools do not support.
	Receipt      string  `json:"receipt,omitempty"`
	Verification *Status `json:"verification,omitempty"`
}

// RunCheck runs the Foundation analyzer over a workspace and returns its
// diagnostics. It runs the same analyzer the CLI runs, in process, so the
// answer cannot drift from what foundation check would say.
func RunCheck(ctx context.Context, dir string, patterns []string) (*CheckResult, error) {
	resolved, err := resolveDir(dir)
	if err != nil {
		return nil, err
	}
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	cfg := &packages.Config{
		Context: ctx,
		Dir:     resolved,
		Mode:    packages.LoadAllSyntax,
		Tests:   false,
	}
	loaded, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("mcp: load packages: %w", err)
	}

	result := &CheckResult{Directory: resolved, Patterns: patterns}
	var loadErrors []string
	for _, pkg := range loaded {
		for _, loadErr := range pkg.Errors {
			loadErrors = append(loadErrors, loadErr.Error())
		}
	}
	if len(loadErrors) > 0 {
		sort.Strings(loadErrors)
		result.Summary = "the packages do not compile, so the analyzer could not run: " +
			strings.Join(loadErrors, "; ")
		return result, nil
	}

	graph, err := checker.Analyze([]*analysis.Analyzer{analyzer.Analyzer}, loaded, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: analyze: %w", err)
	}

	for action := range graph.All() {
		if action.Err != nil {
			return nil, fmt.Errorf("mcp: analyze %s: %w", action.Package.PkgPath, action.Err)
		}
		if !action.IsRoot {
			continue
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				File:    position.Filename,
				Line:    position.Line,
				Column:  position.Column,
				Message: diagnostic.Message,
			})
		}
	}

	sort.Slice(result.Diagnostics, func(i, j int) bool {
		left, right := result.Diagnostics[i], result.Diagnostics[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Column < right.Column
	})

	result.Passed = len(result.Diagnostics) == 0
	if result.Passed {
		result.Summary = "foundation check reported no problems"
	} else {
		result.Summary = fmt.Sprintf(
			"foundation check reported %d problem(s); call foundation_checks for the cause and the fix of each message",
			len(result.Diagnostics),
		)
	}
	return result, nil
}

// Generate writes or verifies the static registries of a workspace.
func Generate(ctx context.Context, dir string, patterns []string, checkOnly bool) (*Step, []string, error) {
	resolved, err := resolveDir(dir)
	if err != nil {
		return nil, nil, err
	}
	if checkOnly {
		step := &Step{Name: "generate -check", Command: "foundation generate -check " + patternList(patterns)}
		if err := generator.Check(ctx, resolved, patterns...); err != nil {
			step.Output = err.Error()
			return step, nil, nil
		}
		step.Passed = true
		step.Output = "generated files match the source"
		return step, nil, nil
	}

	paths, err := generator.Write(ctx, resolved, patterns...)
	step := &Step{Name: "generate", Command: "foundation generate " + patternList(patterns)}
	if err != nil {
		step.Output = err.Error()
		return step, nil, nil
	}
	step.Passed = true
	if len(paths) == 0 {
		step.Output = "no package declares handlers, actions, or contracts, so nothing was generated"
	} else {
		step.Output = fmt.Sprintf("wrote %d file(s)", len(paths))
	}
	return step, paths, nil
}

// Verify runs the whole gate: build, vet, the Foundation analyzer, the
// generated file check, and the tests. It exists so that "it works" stops being
// a claim and becomes the output of commands that ran.
func Verify(ctx context.Context, dir string, withRace bool) (*VerifyResult, error) {
	resolved, err := resolveDir(dir)
	if err != nil {
		return nil, err
	}
	result := &VerifyResult{Directory: resolved, Passed: true}

	build := runCommand(ctx, resolved, "build all packages", "go", "build", "./...")
	result.Steps = append(result.Steps, build)

	if !build.Passed {
		result.Passed = false
		result.Summary = "the workspace does not compile, so nothing else was run"
		result.NextActions = []string{
			"Fix the compilation errors above.",
			"If a symbol is missing, call foundation_symbol before guessing its name.",
			"If RegisterFoundation is undefined, run foundation_generate first.",
		}
		return result, nil
	}

	checked, err := RunCheck(ctx, resolved, nil)
	if err != nil {
		return nil, err
	}
	result.Diagnostics = checked.Diagnostics
	result.Steps = append(result.Steps, Step{
		Name:    "foundation check",
		Command: "foundation check ./...",
		Passed:  checked.Passed,
		Output:  checked.Summary,
	})
	if !checked.Passed {
		result.Passed = false
	}

	generated, _, err := Generate(ctx, resolved, nil, true)
	if err != nil {
		return nil, err
	}
	result.Steps = append(result.Steps, *generated)
	if !generated.Passed {
		result.Passed = false
	}

	vet := runCommand(ctx, resolved, "vet", "go", "vet", "./...")
	result.Steps = append(result.Steps, vet)
	if !vet.Passed {
		result.Passed = false
	}

	testArgs := []string{"test", "./..."}
	name := "tests"
	if withRace {
		testArgs = []string{"test", "-race", "./..."}
		name = "tests with the race detector"
	}
	tests := runCommand(ctx, resolved, name, "go", testArgs...)
	result.Steps = append(result.Steps, tests)
	if !tests.Passed {
		result.Passed = false
	}

	if result.Passed {
		result.Summary = "build, foundation check, generated files, vet, and tests all passed"
		return result, nil
	}

	result.Summary = "verification failed, see the steps"
	for _, step := range result.Steps {
		if step.Passed {
			continue
		}
		switch step.Name {
		case "foundation check":
			result.NextActions = append(result.NextActions,
				"Call foundation_checks with the reported message to get its cause and fix.")
		case "generate -check":
			result.NextActions = append(result.NextActions,
				"Run foundation_generate to update the registries, then commit the generated files.")
		default:
			result.NextActions = append(result.NextActions,
				"Read the output of the failing step and fix the code before reporting success.")
		}
	}
	return result, nil
}

func runCommand(ctx context.Context, dir, name, command string, args ...string) Step {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	step := Step{Name: name, Command: command + " " + strings.Join(args, " ")}
	if err := cmd.Run(); err != nil {
		step.Output = strings.TrimSpace(output.String())
		if step.Output == "" {
			step.Output = err.Error()
		}
		return step
	}
	step.Passed = true
	step.Output = strings.TrimSpace(output.String())
	return step
}

func resolveDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("mcp: resolve %q: %w", dir, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("mcp: %q is not reachable: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("mcp: %q is not a directory", dir)
	}
	return absolute, nil
}

func patternList(patterns []string) string {
	if len(patterns) == 0 {
		return "./..."
	}
	return strings.Join(patterns, " ")
}
