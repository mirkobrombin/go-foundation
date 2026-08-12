package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mirkobrombin/go-foundation/dev/v2/audit"
	"github.com/mirkobrombin/go-foundation/dev/v2/catalog"
	"github.com/mirkobrombin/go-foundation/dev/v2/version"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is reported during the protocol handshake. It is read from the build
// rather than declared here: a constant is accurate until the release someone
// forgets to bump, and a server that misreports its own version undermines
// every answer it gives about which version it speaks for.
var Version = version.Current()

// instructions reach every connected client during initialisation. They are the
// contract of use: a model that reads them knows what it must not improvise.
const instructions = `Foundation is a Go application foundation. This server answers from the API
catalog extracted from the exact version it ships with, and it runs the real
analyzer, generator, compiler, and tests.

Rules of engagement, in order:

1. Never write Foundation code from memory. Call foundation_symbol or
   foundation_package_api first. If a symbol is not in the catalog, it does not
   exist in this version, whatever another source says.
2. Never invent struct tags, route syntax, or wiring. Call
   foundation_declaration_rules for the topic you are about to write.
3. Never present work as finished without calling foundation_verify. It builds,
   analyses, checks the generated registries, vets, and tests, and it reports
   what actually happened. When it passes it issues a receipt bound to the code
   it ran on. Quote that receipt in your report. Editing anything after it voids
   it, and foundation_receipt will say so to whoever asks. A claim of success
   without a current receipt is a claim these tools contradict.
4. When a diagnostic appears, call foundation_checks for its cause and fix
   instead of guessing a change that silences it.
5. Generated registries belong in version control. Run foundation_generate after
   adding or removing a handler, an action, or a contract marker.
6. If wiring crosses packages or is built at runtime, say so: the analyzer is
   deliberately local, and App.Build is where those relationships are decided.

Start with foundation_overview if this is the first call of a session.`

// Serve runs the Foundation MCP server over stdio until the context ends or the
// client disconnects. The workspace is the directory used when a tool call does
// not name one.
func Serve(ctx context.Context, workspace string) error {
	server, err := NewServer(workspace)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcpsdk.StdioTransport{})
}

// NewServer builds the server with every tool, resource, and prompt registered.
func NewServer(workspace string) (*mcpsdk.Server, error) {
	if _, err := API(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspace) == "" {
		workspace = "."
	}

	server := mcpsdk.NewServer(
		&mcpsdk.Implementation{
			Name:       "foundation",
			Title:      "Foundation for Go",
			Version:    Version,
			WebsiteURL: "https://github.com/mirkobrombin/go-foundation",
		},
		&mcpsdk.ServerOptions{Instructions: instructions},
	)

	state := newSession()
	registerKnowledgeTools(server)
	registerWorkspaceTools(server, workspace, state)
	registerResources(server)
	registerPrompts(server)
	return server, nil
}

// --- knowledge tools ---------------------------------------------------------

type emptyInput struct{}

type overviewOutput struct {
	Module        string   `json:"module"`
	GoVersion     string   `json:"go_version"`
	Packages      int      `json:"packages"`
	Symbols       int      `json:"symbols"`
	Layers        []string `json:"layers"`
	Workflow      []string `json:"workflow"`
	Install       []string `json:"install"`
	Rules         []string `json:"rules_of_engagement"`
	Topics        []string `json:"declaration_topics"`
	Documents     []string `json:"documents"`
	NeverDoThis   []string `json:"never_do_this"`
	ToolsToUse    []string `json:"tools"`
	GeneratedFile string   `json:"generated_file"`
}

type packagesInput struct {
	Layer string `json:"layer,omitempty" jsonschema:"filter by layer: core, app, example or root"`
}

type packageSummary struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Layer   string `json:"layer"`
	Doc     string `json:"doc,omitempty"`
	Symbols int    `json:"symbols"`
}

type packagesOutput struct {
	Module   string           `json:"module"`
	Packages []packageSummary `json:"packages"`
	Hint     string           `json:"hint"`
}

type packageAPIInput struct {
	Package string `json:"package" jsonschema:"import path, or its tail such as app/web or web"`
}

type symbolInput struct {
	Query string `json:"query" jsonschema:"symbol name or fragment, for example RegisterHTTP or NewKey"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum matches, default 40"`
}

type symbolOutput struct {
	Query   string        `json:"query"`
	Matches []SymbolMatch `json:"matches"`
	Hint    string        `json:"hint"`
}

type rulesInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"declaration topic; omit to list every topic"`
}

type rulesOutput struct {
	Topics []string `json:"topics"`
	Rule   *Rule    `json:"rule,omitempty"`
	Rules  []Rule   `json:"rules,omitempty"`
}

type checksInput struct {
	Message  string `json:"message,omitempty" jsonschema:"a diagnostic message, or part of it"`
	Category string `json:"category,omitempty" jsonschema:"filter by category"`
}

type checksOutput struct {
	Categories []string `json:"categories"`
	Checks     []Check  `json:"checks"`
	Directives []string `json:"ignore_directives"`
	Hint       string   `json:"hint"`
}

type installOutput struct {
	Module   string   `json:"module"`
	Steps    []string `json:"steps"`
	Config   string   `json:"mcp_client_configuration"`
	Notes    []string `json:"notes"`
	Verified []string `json:"verify_installation"`
}

func registerKnowledgeTools(server *mcpsdk.Server) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_overview",
		Title: "What Foundation is and how to work with it",
		Description: "Start here. Returns the module identity, the layers, the required workflow, " +
			"the rules a model must follow, and the list of every other tool. Call it once per session " +
			"before writing Foundation code.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, overviewOutput, error) {
		loaded, err := API()
		if err != nil {
			return nil, overviewOutput{}, err
		}
		symbols, err := SymbolCount()
		if err != nil {
			return nil, overviewOutput{}, err
		}
		documents, err := DocumentNames()
		if err != nil {
			return nil, overviewOutput{}, err
		}
		workflow, err := RuleByTopic("workflow")
		if err != nil {
			return nil, overviewOutput{}, err
		}
		return nil, overviewOutput{
			Module:    loaded.Module,
			GoVersion: loaded.Go,
			Packages:  len(loaded.Packages),
			Symbols:   symbols,
			Layers: []string{
				"core: runtime building blocks, no application dependency",
				"app: composition and boundaries, di web actions dispatcher hosting testing",
				"dev: analyzer, generator, CLI and this server, a separate module",
			},
			Workflow: workflow.Grammar,
			Install: []string{
				"go get github.com/mirkobrombin/go-foundation/v2@latest",
				"go install github.com/mirkobrombin/go-foundation/dev/v2/cmd/foundation@latest",
			},
			Rules: []string{
				"Look up every symbol before using it. The catalog is the truth for this version.",
				"Read the declaration rules before writing tags or routes.",
				"Run foundation_verify before claiming anything works, and quote the receipt it issues.",
				"Commit generated registries.",
				"Say plainly when a relationship can only be checked at build time.",
			},
			Topics:    RuleTopics(),
			Documents: documents,
			NeverDoThis: []string{
				"Do not invent a function, a method, or a package path. Search the catalog.",
				"Do not copy v1 code shapes: injection now needs an explicit inject tag and registration returns errors.",
				"Do not silence a diagnostic with an ignore directive to make a build pass.",
				"Do not edit zz_foundation.gen.go by hand.",
				"Do not report success from reading code. Report it from foundation_verify output, receipt included.",
				"Do not reuse a receipt after editing a file. It is void, and foundation_receipt will say so.",
			},
			ToolsToUse: []string{
				"foundation_packages, foundation_package_api, foundation_symbol: the API as it exists",
				"foundation_declaration_rules: tags, routes, wiring, contracts, generation",
				"foundation_checks: every diagnostic with its cause and fix",
				"foundation_scaffold: a correct project to start from",
				"foundation_check, foundation_generate, foundation_verify: run the real tools",
				"foundation_migrate: v1 to v2",
				"foundation_install: install and client configuration",
			},
			GeneratedFile: "zz_foundation.gen.go, one per package that declares handlers, actions, or contracts",
		}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "foundation_packages",
		Title:       "List Foundation packages",
		Description: "Every importable package of the runtime module, with its layer and purpose.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in packagesInput) (*mcpsdk.CallToolResult, packagesOutput, error) {
		loaded, err := API()
		if err != nil {
			return nil, packagesOutput{}, err
		}
		layer := strings.ToLower(strings.TrimSpace(in.Layer))
		out := packagesOutput{
			Module: loaded.Module,
			Hint:   "call foundation_package_api for the full API of one package",
		}
		for _, pkg := range loaded.Packages {
			if layer != "" && pkg.Layer != layer {
				continue
			}
			out.Packages = append(out.Packages, packageSummary{
				Path:    pkg.Path,
				Name:    pkg.Name,
				Layer:   pkg.Layer,
				Doc:     pkg.Doc,
				Symbols: len(pkg.Symbols),
			})
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_package_api",
		Title: "Full API of one package",
		Description: "Every exported declaration of a package with its signature, documentation, " +
			"struct fields with their tags, and methods. Use it before writing code against a package.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in packageAPIInput) (*mcpsdk.CallToolResult, catalog.Package, error) {
		pkg, err := PackageByPath(in.Package)
		if err != nil {
			return nil, catalog.Package{}, err
		}
		return nil, *pkg, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_symbol",
		Title: "Find a symbol",
		Description: "Search the API catalog by name. Use it whenever you are about to write a Foundation " +
			"identifier you have not just read. An empty result means the symbol does not exist in this version.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in symbolInput) (*mcpsdk.CallToolResult, symbolOutput, error) {
		matches, err := SearchSymbols(in.Query, in.Limit)
		if err != nil {
			return nil, symbolOutput{}, err
		}
		hint := "these signatures are extracted from the source of this version"
		if len(matches) == 0 {
			hint = "no symbol matches; it does not exist in this version, do not use it"
		}
		return nil, symbolOutput{Query: in.Query, Matches: matches, Hint: hint}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_declaration_rules",
		Title: "Declaration grammar and rules",
		Description: "The exact grammar Foundation understands for handlers, actions, dependency injection, " +
			"contracts, binding, errors, typed APIs, layering, scheduling, generation, the workflow, and v1 migration. " +
			"Includes the mistakes each topic invites.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in rulesInput) (*mcpsdk.CallToolResult, rulesOutput, error) {
		if strings.TrimSpace(in.Topic) == "" {
			return nil, rulesOutput{Topics: RuleTopics(), Rules: Rules()}, nil
		}
		rule, err := RuleByTopic(in.Topic)
		if err != nil {
			return nil, rulesOutput{}, err
		}
		return nil, rulesOutput{Topics: RuleTopics(), Rule: rule}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_checks",
		Title: "Analyzer diagnostics with cause and fix",
		Description: "Every diagnostic foundation check can report, what causes it, and how to fix it. " +
			"Call it with a message you received instead of guessing a change that silences it.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in checksInput) (*mcpsdk.CallToolResult, checksOutput, error) {
		out := checksOutput{
			Categories: CheckCategories(),
			Directives: IgnoreDirectives(),
			Hint:       "a fix that removes the diagnostic without removing the cause moves the failure to runtime",
		}
		message := strings.ToLower(strings.TrimSpace(in.Message))
		if message == "" {
			out.Checks = ChecksByCategory(in.Category)
			return nil, out, nil
		}
		for _, check := range ChecksByCategory(in.Category) {
			pattern := strings.ToLower(check.Message)
			if matchesDiagnostic(pattern, message) {
				out.Checks = append(out.Checks, check)
			}
		}
		if len(out.Checks) == 0 {
			out.Checks = ChecksByCategory(in.Category)
			out.Hint = "no catalog entry matched that text; the full list for the category is returned instead"
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "foundation_install",
		Title:       "Install Foundation and this server",
		Description: "The exact commands to add the module, install the CLI, and wire this MCP server into a client.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, installOutput, error) {
		loaded, err := API()
		if err != nil {
			return nil, installOutput{}, err
		}
		return nil, installOutput{
			Module: loaded.Module,
			Steps: []string{
				"go get github.com/mirkobrombin/go-foundation/v2@latest",
				"go install github.com/mirkobrombin/go-foundation/dev/v2/cmd/foundation@latest",
				"foundation version",
			},
			Config: `{
  "mcpServers": {
    "foundation": {
      "command": "foundation",
      "args": ["mcp"]
    }
  }
}`,
			Notes: []string{
				"The tools module is tagged separately as dev/vX.Y.Z, so @latest resolves against those tags.",
				"The runtime module has no third-party dependencies; the tools are a development dependency only.",
				"The server speaks MCP over stdio and needs no network access.",
				"Run the server from the workspace you are working on, or pass -workspace to point it elsewhere.",
			},
			Verified: []string{
				"foundation version prints the tool version",
				"foundation check ./... runs in a Go module",
				"calling foundation_overview through this server returns a package count above zero",
			},
		}, nil
	})
}

func matchesDiagnostic(pattern, message string) bool {
	parts := strings.FieldsFunc(pattern, func(r rune) bool { return r == '%' })
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(strings.TrimLeft(part, "sqvd "))
		if len(trimmed) >= 4 {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return strings.Contains(message, strings.TrimSpace(pattern))
	}
	for _, part := range cleaned {
		if !strings.Contains(message, part) {
			return false
		}
	}
	return true
}

// --- workspace tools ---------------------------------------------------------

type checkWorkspaceInput struct {
	Directory string   `json:"directory,omitempty" jsonschema:"module directory, defaults to the server workspace"`
	Patterns  []string `json:"patterns,omitempty" jsonschema:"package patterns, defaults to ./..."`
}

type generateInput struct {
	Directory string   `json:"directory,omitempty" jsonschema:"module directory, defaults to the server workspace"`
	Patterns  []string `json:"patterns,omitempty" jsonschema:"package patterns, defaults to ./..."`
	CheckOnly bool     `json:"check_only,omitempty" jsonschema:"verify committed files instead of writing them"`
}

type generateOutput struct {
	Step         Step     `json:"step"`
	Files        []string `json:"files,omitempty"`
	Note         string   `json:"note"`
	Verification *Status  `json:"verification,omitempty"`
}

type receiptInput struct {
	Directory string `json:"directory,omitempty" jsonschema:"module directory, defaults to the server workspace"`
	Receipt   string `json:"receipt,omitempty" jsonschema:"a receipt issued by foundation_verify; omit to ask for the current standing"`
}

type verifyInput struct {
	Directory string `json:"directory,omitempty" jsonschema:"module directory, defaults to the server workspace"`
	Race      bool   `json:"race,omitempty" jsonschema:"run the tests under the race detector"`
	Audit     bool   `json:"audit,omitempty" jsonschema:"also run the supply chain scan; needs the network and takes longer"`
}

type scaffoldInput struct {
	Directory  string `json:"directory" jsonschema:"target directory, created if missing"`
	Module     string `json:"module" jsonschema:"Go module path, for example example.com/service"`
	WithServer bool   `json:"with_server,omitempty" jsonschema:"generate a main that listens instead of only building"`
}

type auditInput struct {
	Directory    string `json:"directory,omitempty" jsonschema:"project directory, defaults to the server workspace"`
	Name         string `json:"name,omitempty" jsonschema:"project name recorded in the SBOM"`
	Version      string `json:"version,omitempty" jsonschema:"project version recorded in the SBOM"`
	Offline      bool   `json:"offline,omitempty" jsonschema:"skip every network step; the result is always reported as incomplete"`
	SkipCodeScan bool   `json:"skip_code_scan,omitempty" jsonschema:"skip the static analysis pass over the source"`
	SBOMPath     string `json:"sbom_path,omitempty" jsonschema:"write the CycloneDX document to this path"`
}

type migrateInput struct {
	Symbol string `json:"symbol,omitempty" jsonschema:"a v1 package or symbol, for example pkg/srv or testutil"`
}

type migrateOutput struct {
	Mapping   map[string]string `json:"package_mapping"`
	Match     string            `json:"match,omitempty"`
	Steps     []string          `json:"steps"`
	Behaviour []string          `json:"behaviour_changes"`
	Note      string            `json:"note"`
}

func registerWorkspaceTools(server *mcpsdk.Server, workspace string, state *session) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_check",
		Title: "Run the Foundation analyzer",
		Description: "Runs the real analyzer over a module and returns structured diagnostics. " +
			"It reports wiring that compiles and still cannot work, so it is not optional. " +
			"Passing this is not verification on its own: only foundation_verify issues a receipt.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in checkWorkspaceInput) (*mcpsdk.CallToolResult, CheckResult, error) {
		directory := orDefault(in.Directory, workspace)
		result, err := RunCheck(ctx, directory, in.Patterns)
		if err != nil {
			return nil, CheckResult{}, err
		}
		status := state.status(result.Directory)
		result.Verification = &status
		return nil, *result, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_generate",
		Title: "Write or verify the static registries",
		Description: "Runs the generator. Without check_only it writes zz_foundation.gen.go per declaring package " +
			"and removes orphaned ones; with check_only it fails when a committed file is stale.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in generateInput) (*mcpsdk.CallToolResult, generateOutput, error) {
		directory := orDefault(in.Directory, workspace)
		step, files, err := Generate(ctx, directory, in.Patterns, in.CheckOnly)
		if err != nil {
			return nil, generateOutput{}, err
		}
		resolved, resolveErr := resolveDir(directory)
		if resolveErr == nil && !in.CheckOnly && len(files) > 0 {
			state.recordWrite(resolved)
			state.clearVerification(resolved)
		}
		out := generateOutput{
			Step:  *step,
			Files: files,
			Note:  "commit generated files together with the declarations that produced them",
		}
		if resolveErr == nil {
			status := state.status(resolved)
			out.Verification = &status
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_verify",
		Title: "Build, analyse, check registries, vet and test",
		Description: "The gate before reporting work as done. Runs go build, the Foundation analyzer, " +
			"foundation generate -check, go vet and go test, and returns what each one said. " +
			"On success it issues a receipt bound to the code it verified: quote that receipt when you report, " +
			"because it is void the moment a file changes. Reporting success without a current receipt is an " +
			"unverified claim, and foundation_receipt will show it.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in verifyInput) (*mcpsdk.CallToolResult, VerifyResult, error) {
		directory := orDefault(in.Directory, workspace)
		result, err := Verify(ctx, directory, in.Race)
		if err != nil {
			return nil, VerifyResult{}, err
		}
		if in.Audit {
			scan, auditErr := audit.Run(ctx, audit.Options{Directory: result.Directory})
			step := Step{Name: "supply chain audit", Command: "foundation audit"}
			switch {
			case auditErr != nil:
				step.Output = auditErr.Error()
			default:
				step.Passed = scan.Passed
				step.Output = scan.Summary
			}
			result.Steps = append(result.Steps, step)
			if !step.Passed {
				result.Passed = false
				result.NextActions = append(result.NextActions,
					"Call foundation_audit for the findings, and treat an incomplete scan as unresolved rather than clean.")
			}
		}
		if result.Passed {
			if fingerprint, err := Fingerprint(result.Directory); err == nil {
				result.Receipt = state.recordVerification(result.Directory, fingerprint)
			}
		} else {
			state.clearVerification(result.Directory)
		}
		status := state.status(result.Directory)
		result.Verification = &status
		if !result.Passed {
			result.NextActions = append(result.NextActions,
				"No receipt was issued. Do not report this work as done.")
		}
		return nil, *result, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_receipt",
		Title: "Check a verification receipt",
		Description: "Tells whether a receipt still matches the code as it stands, or asks for the current " +
			"standing when no receipt is given. A receipt issued before the last edit is void, which is how " +
			"a stale claim of success becomes visible.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in receiptInput) (*mcpsdk.CallToolResult, ReceiptStanding, error) {
		directory := orDefault(in.Directory, workspace)
		if strings.TrimSpace(in.Receipt) == "" {
			resolved, err := resolveDir(directory)
			if err != nil {
				return nil, ReceiptStanding{}, err
			}
			status := state.status(resolved)
			return nil, ReceiptStanding{
				Receipt:   status.Receipt,
				Directory: resolved,
				Valid:     status.State == stateCurrent,
				State:     status.State,
				Verdict:   status.Guidance,
			}, nil
		}
		standing, err := CheckReceipt(directory, in.Receipt)
		if err != nil {
			return nil, ReceiptStanding{}, err
		}
		return nil, *standing, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_scaffold",
		Title: "Create a Foundation project",
		Description: "Writes a project whose shapes are the ones the analyzer and the generator expect: " +
			"a contract, an implementation with a marker, an HTTP handler, an action, the wiring, and a test. " +
			"Refuses to overwrite existing files.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in scaffoldInput) (*mcpsdk.CallToolResult, ScaffoldResult, error) {
		result, err := Scaffold(ctx, in.Directory, in.Module, in.WithServer)
		if err != nil {
			return nil, ScaffoldResult{}, err
		}
		state.recordWrite(result.Directory)
		state.clearVerification(result.Directory)
		status := state.status(result.Directory)
		result.Verification = &status
		return nil, *result, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "foundation_audit",
		Title: "Scan dependencies and code for known vulnerabilities",
		Description: "Runs the supply chain scan in process: reads every dependency manifest, matches them " +
			"against the live vulnerability databases, and scans the source for risky patterns. " +
			"Read the result with care: passed is false both when something was found and when the scan " +
			"could not complete, because an empty finding list from a scan that never reached the databases " +
			"is not an all clear. The reasons live in degraded.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in auditInput) (*mcpsdk.CallToolResult, audit.Result, error) {
		result, err := audit.Run(ctx, audit.Options{
			Directory: orDefault(in.Directory, workspace),
			Name:      in.Name,
			Version:   in.Version,
			Offline:   in.Offline,
			SkipSAST:  in.SkipCodeScan,
			SBOMPath:  in.SBOMPath,
		})
		if err != nil {
			return nil, audit.Result{}, err
		}
		return nil, *result, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "foundation_migrate",
		Title:       "Migrate from v1 to v2",
		Description: "Where a v1 package or symbol went in v2, plus the behaviour changes that a rewrite of imports alone will miss.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in migrateInput) (*mcpsdk.CallToolResult, migrateOutput, error) {
		rule, err := RuleByTopic("migration_v1")
		if err != nil {
			return nil, migrateOutput{}, err
		}
		mapping := map[string]string{
			"pkg/app":        "app",
			"pkg/di":         "app/di",
			"pkg/actions":    "app/actions",
			"pkg/dispatcher": "app/dispatcher",
			"pkg/hosting":    "app/hosting",
			"pkg/srv":        "app/web (package web)",
			"pkg/testutil":   "app/testing (package apptest)",
			"pkg/<other>":    "core/<other>",
		}
		out := migrateOutput{
			Mapping:   mapping,
			Steps:     rule.Grammar,
			Behaviour: rule.Constraints,
			Note:      "reflection based registration still exists, so the move can be done in one step and the static form adopted later",
		}
		needle := strings.ToLower(strings.TrimSpace(in.Symbol))
		if needle != "" {
			for from, to := range mapping {
				if strings.Contains(strings.ToLower(from), needle) || strings.Contains(needle, strings.TrimPrefix(strings.ToLower(from), "pkg/")) {
					out.Match = fmt.Sprintf("%s -> %s", from, to)
					break
				}
			}
			if out.Match == "" {
				out.Match = fmt.Sprintf("no direct mapping for %q; every other pkg/<name> became core/<name>, confirm with foundation_symbol", in.Symbol)
			}
		}
		return nil, out, nil
	})
}

// --- resources and prompts ---------------------------------------------------

func registerResources(server *mcpsdk.Server) {
	names, err := DocumentNames()
	if err != nil {
		return
	}
	for _, name := range names {
		documentName := name
		server.AddResource(&mcpsdk.Resource{
			URI:         "foundation://docs/" + documentName,
			Name:        documentName,
			Title:       "Foundation documentation: " + documentName,
			Description: "Project documentation shipped with this version of Foundation.",
			MIMEType:    "text/markdown",
		}, func(ctx context.Context, request *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
			content, err := Document(documentName)
			if err != nil {
				return nil, err
			}
			return &mcpsdk.ReadResourceResult{
				Contents: []*mcpsdk.ResourceContents{{
					URI:      request.Params.URI,
					MIMEType: "text/markdown",
					Text:     content,
				}},
			}, nil
		})
	}
}

func registerPrompts(server *mcpsdk.Server) {
	server.AddPrompt(&mcpsdk.Prompt{
		Name:        "foundation_new_service",
		Title:       "Start a Foundation service",
		Description: "The procedure for creating a Foundation application without improvising any of it.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "module", Description: "Go module path", Required: true},
			{Name: "directory", Description: "Target directory", Required: false},
		},
	}, func(ctx context.Context, request *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		module := request.Params.Arguments["module"]
		directory := orDefault(request.Params.Arguments["directory"], ".")
		text := fmt.Sprintf(`Create a Foundation service in %s with module %s.

Follow this order and do not skip a step:

1. foundation_overview, to load the rules.
2. foundation_scaffold with directory %s and module %s.
3. go get the runtime module, then foundation_generate.
4. Write the domain code. Before each Foundation identifier, foundation_symbol.
   Before each struct tag or route, foundation_declaration_rules.
5. foundation_verify. If it fails, foundation_checks on the message, fix, repeat.
6. Report the result quoting the foundation_verify output. Do not claim success
   from reading the code.`, directory, module, directory, module)
		return &mcpsdk.GetPromptResult{
			Description: "Create a Foundation service",
			Messages: []*mcpsdk.PromptMessage{{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			}},
		}, nil
	})

	server.AddPrompt(&mcpsdk.Prompt{
		Name:        "foundation_migrate_v1",
		Title:       "Migrate an application from v1 to v2",
		Description: "The procedure for a v1 to v2 migration, which is a wiring migration and not an import rewrite.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "directory", Description: "Application directory", Required: false},
		},
	}, func(ctx context.Context, request *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		directory := orDefault(request.Params.Arguments["directory"], ".")
		text := fmt.Sprintf(`Migrate the Foundation application in %s from v1 to v2.

1. foundation_migrate, to load the mapping and the behaviour changes.
2. Update the module path and the imports.
3. Rename the packages that changed name: srv became web, testutil became apptest.
4. Handle the errors that registration and injection now return.
5. Add inject:"name" to every field that used to be injected implicitly. Untagged
   fields are no longer filled, and this is the change that breaks silently.
6. foundation_generate, then foundation_check.
7. foundation_verify with race enabled.
8. Report what changed and what you could not verify statically, cross package
   wiring in particular.`, directory)
		return &mcpsdk.GetPromptResult{
			Description: "Migrate from v1 to v2",
			Messages: []*mcpsdk.PromptMessage{{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			}},
		}, nil
	})

	server.AddPrompt(&mcpsdk.Prompt{
		Name:        "foundation_review",
		Title:       "Review Foundation code",
		Description: "Review an application against the rules this server enforces.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "directory", Description: "Application directory", Required: false},
		},
	}, func(ctx context.Context, request *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		directory := orDefault(request.Params.Arguments["directory"], ".")
		text := fmt.Sprintf(`Review the Foundation application in %s.

1. foundation_verify, and treat its output as the starting point rather than your reading.
2. For every diagnostic, foundation_checks for the cause, then judge whether the
   code or the declaration is wrong.
3. Check the declarations against foundation_declaration_rules: route parameters
   with matching fields, exported injected fields with explicit tags, contract
   markers on interfaces, handled registration errors.
4. Confirm the generated registries are committed and current.
5. Report findings with file and line, and separate what you verified by running
   something from what you inferred by reading.`, directory)
		return &mcpsdk.GetPromptResult{
			Description: "Review Foundation code",
			Messages: []*mcpsdk.PromptMessage{{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			}},
		}, nil
	})
}
