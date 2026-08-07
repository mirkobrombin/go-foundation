package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires a client to the server over an in-memory transport, which
// exercises the real protocol: initialisation, tool listing, and tool calls.
func connect(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()

	server, err := NewServer(".")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *mcpsdk.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool(%s) reported an error: %+v", name, result.Content)
	}
	decoded := map[string]any{}
	if result.StructuredContent != nil {
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("marshal structured content: %v", err)
		}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("decode structured content: %v", err)
		}
	}
	return decoded
}

func TestServerExposesEveryTool(t *testing.T) {
	session := connect(t)

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	found := map[string]bool{}
	for _, tool := range result.Tools {
		found[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
	}

	for _, want := range []string{
		"foundation_overview",
		"foundation_packages",
		"foundation_package_api",
		"foundation_symbol",
		"foundation_declaration_rules",
		"foundation_checks",
		"foundation_install",
		"foundation_check",
		"foundation_generate",
		"foundation_verify",
		"foundation_scaffold",
		"foundation_migrate",
		"foundation_receipt",
		"foundation_audit",
	} {
		if !found[want] {
			t.Errorf("tool %s is missing", want)
		}
	}
}

func TestOverviewReportsTheLoadedCatalog(t *testing.T) {
	session := connect(t)
	out := callTool(t, session, "foundation_overview", nil)

	if out["module"] != "github.com/mirkobrombin/go-foundation/v2" {
		t.Fatalf("module = %v", out["module"])
	}
	packages, _ := out["packages"].(float64)
	if packages < 40 {
		t.Fatalf("packages = %v, the catalog looks empty", out["packages"])
	}
	symbols, _ := out["symbols"].(float64)
	if symbols < 500 {
		t.Fatalf("symbols = %v, the catalog looks truncated", out["symbols"])
	}
}

func TestSymbolLookupAnswersFromTheCatalog(t *testing.T) {
	session := connect(t)

	out := callTool(t, session, "foundation_symbol", map[string]any{"query": "RegisterHTTP"})
	matches, _ := out["matches"].([]any)
	if len(matches) == 0 {
		t.Fatal("RegisterHTTP was not found, the catalog is not being consulted")
	}

	missing := callTool(t, session, "foundation_symbol", map[string]any{"query": "RegisterMagicHandler"})
	empty, _ := missing["matches"].([]any)
	if len(empty) != 0 {
		t.Fatalf("an invented symbol matched: %v", empty)
	}
	hint, _ := missing["hint"].(string)
	if !strings.Contains(hint, "does not exist") {
		t.Fatalf("hint = %q, it must tell the caller the symbol does not exist", hint)
	}
}

func TestPackageAPIReturnsSignaturesAndTags(t *testing.T) {
	session := connect(t)
	out := callTool(t, session, "foundation_package_api", map[string]any{"package": "app/web"})

	symbols, _ := out["symbols"].([]any)
	if len(symbols) == 0 {
		t.Fatal("app/web returned no symbols")
	}
	encoded, _ := json.Marshal(out)
	if !strings.Contains(string(encoded), "HandlerDefinition") {
		t.Fatal("app/web does not expose HandlerDefinition, the extraction is wrong")
	}
}

func TestDeclarationRulesCoverEveryTopic(t *testing.T) {
	session := connect(t)

	all := callTool(t, session, "foundation_declaration_rules", nil)
	topics, _ := all["topics"].([]any)
	if len(topics) < 10 {
		t.Fatalf("topics = %v", topics)
	}

	one := callTool(t, session, "foundation_declaration_rules", map[string]any{"topic": "http_handler"})
	rule, _ := one["rule"].(map[string]any)
	if rule == nil {
		t.Fatal("http_handler returned no rule")
	}
	grammar, _ := rule["grammar"].([]any)
	if len(grammar) == 0 {
		t.Fatal("http_handler has no grammar")
	}
	example, _ := rule["example"].(string)
	if !strings.Contains(example, `method:"GET"`) {
		t.Fatalf("the http_handler example does not show the route tag: %q", example)
	}
}

func TestChecksExplainADiagnostic(t *testing.T) {
	session := connect(t)
	out := callTool(t, session, "foundation_checks", map[string]any{
		"message": `route parameter "id" has no path field`,
	})

	checks, _ := out["checks"].([]any)
	if len(checks) == 0 {
		t.Fatal("no catalog entry matched a real diagnostic")
	}
	first, _ := checks[0].(map[string]any)
	if first["fix"] == "" || first["cause"] == "" {
		t.Fatalf("entry without cause or fix: %v", first)
	}
}

// TestCheckCatalogIsComplete compares the documented diagnostics against every
// report site in the analyzer. A new diagnostic that ships undocumented fails
// here, which is the only way "nothing is omitted" can stay true over time.
func TestCheckCatalogIsComplete(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "analyzer", "analyzer.go"))
	if err != nil {
		t.Fatalf("read analyzer: %v", err)
	}

	pattern := regexp.MustCompile(`Reportf\(\s*(?:[^,]+,\s*)+?"([^"]+)"`)
	documented := map[string]bool{}
	for _, check := range Checks() {
		documented[check.Message] = true
	}

	var missing []string
	for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
		message := match[1]
		if message == "%v" {
			// Wrapped error, its concrete texts are documented individually.
			continue
		}
		if !documented[message] {
			missing = append(missing, message)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("undocumented analyzer diagnostics: %s", strings.Join(missing, " | "))
	}
}

func TestResourcesAndPromptsAreServed(t *testing.T) {
	session := connect(t)
	ctx := context.Background()

	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.Resources) == 0 {
		t.Fatal("no documentation resources are served")
	}
	read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: resources.Resources[0].URI})
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if len(read.Contents) == 0 || read.Contents[0].Text == "" {
		t.Fatal("a documentation resource is empty")
	}

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts.Prompts) < 3 {
		t.Fatalf("prompts = %d, want the three workflows", len(prompts.Prompts))
	}
	got, err := session.GetPrompt(ctx, &mcpsdk.GetPromptParams{
		Name:      "foundation_new_service",
		Arguments: map[string]string{"module": "example.com/service"},
	})
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if len(got.Messages) == 0 {
		t.Fatal("the prompt has no messages")
	}
	text, ok := got.Messages[0].Content.(*mcpsdk.TextContent)
	if !ok || !strings.Contains(text.Text, "foundation_verify") {
		t.Fatalf("the prompt does not require verification: %+v", got.Messages[0].Content)
	}
}

func TestCheckRunsTheRealAnalyzer(t *testing.T) {
	session := connect(t)

	clean := callTool(t, session, "foundation_check", map[string]any{
		"directory": filepath.Join("..", "..", "examples", "quickstart"),
	})
	if passed, _ := clean["passed"].(bool); !passed {
		t.Fatalf("the quickstart example should be clean: %v", clean["diagnostics"])
	}

	broken := t.TempDir()
	writeBrokenModule(t, broken)
	result := callTool(t, session, "foundation_check", map[string]any{"directory": broken})
	diagnostics, _ := result["diagnostics"].([]any)
	if len(diagnostics) == 0 {
		t.Fatalf("a route parameter without a field was not reported: %v", result)
	}
}

func TestCheckRunsOnFoundationRuntime(t *testing.T) {
	result, err := RunCheck(context.Background(), filepath.Join("..", ".."), []string{"./core/bind"})
	if err != nil {
		t.Fatalf("RunCheck() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("core/bind should be clean: %v", result.Diagnostics)
	}
}

func TestScaffoldWritesACompleteProject(t *testing.T) {
	session := connect(t)
	target := filepath.Join(t.TempDir(), "service")

	out := callTool(t, session, "foundation_scaffold", map[string]any{
		"directory": target,
		"module":    "example.com/service",
	})
	files, _ := out["files"].([]any)
	if len(files) < 5 {
		t.Fatalf("files = %v", files)
	}
	for _, name := range []string{"go.mod", "users.go", "handlers.go", "app.go", "app_test.go"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}

	handlers, err := os.ReadFile(filepath.Join(target, "handlers.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`method:"GET"`, `path:"id"`, `inject:"users"`, `action:"users.create"`} {
		if !strings.Contains(string(handlers), want) {
			t.Errorf("the scaffold does not show %s", want)
		}
	}

	// A tool failure travels inside the result, not as a protocol error.
	again, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "foundation_scaffold",
		Arguments: map[string]any{"directory": target, "module": "example.com/service"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !again.IsError {
		t.Fatal("scaffolding over an existing project must fail")
	}
}

// TestVerificationReceiptBindsToTheCode is the point of the receipt: an
// assistant cannot verify once and then keep editing while quoting the same
// proof. The receipt is bound to the content it ran on, and the tools say so.
func TestVerificationReceiptBindsToTheCode(t *testing.T) {
	session := connect(t)
	project := filepath.Join("..", "..", "examples", "quickstart")

	before := callTool(t, session, "foundation_receipt", map[string]any{"directory": project})
	if before["state"] != stateNone {
		t.Fatalf("state before any verification = %v", before["state"])
	}

	verified := callTool(t, session, "foundation_verify", map[string]any{"directory": project})
	if passed, _ := verified["passed"].(bool); !passed {
		t.Fatalf("the quickstart should verify: %v", verified["steps"])
	}
	receipt, _ := verified["receipt"].(string)
	if !strings.HasPrefix(receipt, receiptPrefix+":") {
		t.Fatalf("receipt = %q", receipt)
	}

	current := callTool(t, session, "foundation_receipt", map[string]any{
		"directory": project,
		"receipt":   receipt,
	})
	if valid, _ := current["valid"].(bool); !valid {
		t.Fatalf("the receipt should be current: %v", current)
	}

	// Editing the workspace must void it, without any further tool call.
	scratch := filepath.Join(project, "zz_receipt_probe.go")
	if err := os.WriteFile(scratch, []byte("package quickstart\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(scratch)

	stale := callTool(t, session, "foundation_receipt", map[string]any{
		"directory": project,
		"receipt":   receipt,
	})
	if valid, _ := stale["valid"].(bool); valid {
		t.Fatal("a receipt survived an edit, so it proves nothing")
	}
	if stale["state"] != stateStale {
		t.Fatalf("state after an edit = %v", stale["state"])
	}
}

func TestUnverifiedWorkIsReportedAsSuch(t *testing.T) {
	session := connect(t)
	target := filepath.Join(t.TempDir(), "service")

	out := callTool(t, session, "foundation_scaffold", map[string]any{
		"directory": target,
		"module":    "example.com/service",
	})
	verification, _ := out["verification"].(map[string]any)
	if verification == nil {
		t.Fatal("scaffolding did not report a verification standing")
	}
	if verification["state"] != stateNone {
		t.Fatalf("state after scaffolding = %v", verification["state"])
	}
	guidance, _ := verification["guidance"].(string)
	if !strings.Contains(guidance, "foundation_verify") {
		t.Fatalf("guidance = %q, it must point at the gate", guidance)
	}

	standing := callTool(t, session, "foundation_receipt", map[string]any{"directory": target})
	if valid, _ := standing["valid"].(bool); valid {
		t.Fatal("a freshly scaffolded project reported itself as verified")
	}
}

func TestForgedReceiptIsRejected(t *testing.T) {
	session := connect(t)
	out := callTool(t, session, "foundation_receipt", map[string]any{
		"directory": filepath.Join("..", "..", "examples", "quickstart"),
		"receipt":   "fv1:0000000000000000",
	})
	if valid, _ := out["valid"].(bool); valid {
		t.Fatal("an invented receipt was accepted")
	}
}

// TestAuditRefusesToCallAnIncompleteScanClean covers the trap of a supply chain
// tool: with no network it finds nothing, and "nothing found" would read as
// "nothing wrong" to whatever is reading the result.
func TestAuditRefusesToCallAnIncompleteScanClean(t *testing.T) {
	session := connect(t)

	project := t.TempDir()
	manifest := "module example.com/audited\n\ngo 1.25\n\nrequire golang.org/x/sys v0.41.0\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	out := callTool(t, session, "foundation_audit", map[string]any{
		"directory":      project,
		"offline":        true,
		"skip_code_scan": true,
	})

	if dependencies, _ := out["dependencies"].(float64); dependencies != 1 {
		t.Fatalf("dependencies = %v, want 1", out["dependencies"])
	}
	if passed, _ := out["passed"].(bool); passed {
		t.Fatal("an offline scan reported itself as passed")
	}
	degraded, _ := out["degraded"].([]any)
	if len(degraded) == 0 {
		t.Fatal("the reason the scan is incomplete was not reported")
	}
	summary, _ := out["summary"].(string)
	if !strings.Contains(summary, "not an all clear") {
		t.Fatalf("summary = %q", summary)
	}
}

func writeBrokenModule(t *testing.T, dir string) {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod": `module example.com/broken

go 1.25

require github.com/mirkobrombin/go-foundation/v2 v2.0.0

replace github.com/mirkobrombin/go-foundation/v2 => ` + root + `
`,
		"handler.go": `package broken

import "context"

type GetUser struct {
	_ struct{} ` + "`method:\"GET\" path:\"/users/{id:int}\"`" + `
}

func (h *GetUser) Handle(context.Context) (any, error) { return nil, nil }
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
