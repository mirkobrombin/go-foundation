// Package catalog extracts the exported API of the Foundation runtime module.
//
// The result is written next to the MCP server and embedded in the CLI, so a
// tool answering questions about Foundation answers from the source of the
// version it ships with, never from memory. Regeneration is deterministic and
// checked in continuous integration, which is what keeps the answer aligned
// with the code as the code moves.
package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/doc"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// File is the catalog location relative to the development module root.
const File = "mcp/api.json"

// API is the exported surface of one module.
type API struct {
	Module   string    `json:"module"`
	Go       string    `json:"go"`
	Packages []Package `json:"packages"`
}

// Package is one importable package.
type Package struct {
	Path    string   `json:"path"`
	Name    string   `json:"name"`
	Layer   string   `json:"layer"`
	Doc     string   `json:"doc,omitempty"`
	Symbols []Symbol `json:"symbols"`
}

// Symbol is an exported declaration.
type Symbol struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Signature string   `json:"signature"`
	Doc       string   `json:"doc,omitempty"`
	Fields    []Field  `json:"fields,omitempty"`
	Methods   []Method `json:"methods,omitempty"`
}

// Field is an exported struct field, including its tag.
type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Tag  string `json:"tag,omitempty"`
	Doc  string `json:"doc,omitempty"`
}

// Method is an exported method of a type.
type Method struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Doc       string `json:"doc,omitempty"`
}

// Extract loads the module rooted at dir and returns its exported API.
func Extract(ctx context.Context, dir string) (*API, error) {
	module, goVersion, err := moduleInfo(dir)
	if err != nil {
		return nil, err
	}

	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedDeps,
	}
	loaded, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("catalog: load: %w", err)
	}

	var failures []string
	for _, pkg := range loaded {
		for _, loadErr := range pkg.Errors {
			failures = append(failures, loadErr.Error())
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return nil, fmt.Errorf("catalog: package loading failed:\n%s", strings.Join(failures, "\n"))
	}

	api := &API{Module: module, Go: goVersion}
	for _, pkg := range loaded {
		if pkg.Types == nil || strings.HasSuffix(pkg.PkgPath, "_test") {
			continue
		}
		entry, err := describe(pkg)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			api.Packages = append(api.Packages, *entry)
		}
	}
	sort.Slice(api.Packages, func(i, j int) bool {
		return api.Packages[i].Path < api.Packages[j].Path
	})
	return api, nil
}

func describe(pkg *packages.Package) (*Package, error) {
	files := make([]*ast.File, 0, len(pkg.Syntax))
	files = append(files, pkg.Syntax...)
	documented, err := doc.NewFromFiles(pkg.Fset, files, pkg.PkgPath)
	if err != nil {
		return nil, fmt.Errorf("catalog: document %s: %w", pkg.PkgPath, err)
	}

	entry := &Package{
		Path:  pkg.PkgPath,
		Name:  pkg.Name,
		Layer: layerOf(pkg.PkgPath),
		Doc:   condense(documented.Doc),
	}

	scope := pkg.Types.Scope()
	qualifier := types.RelativeTo(pkg.Types)

	for _, value := range append(append([]*doc.Value{}, documented.Consts...), documented.Vars...) {
		kind := "var"
		if value.Decl != nil && value.Decl.Tok == token.CONST {
			kind = "const"
		}
		for _, name := range value.Names {
			if !ast.IsExported(name) {
				continue
			}
			object := scope.Lookup(name)
			if object == nil {
				continue
			}
			entry.Symbols = append(entry.Symbols, Symbol{
				Kind:      kind,
				Name:      name,
				Signature: types.ObjectString(object, qualifier),
				Doc:       condense(value.Doc),
			})
		}
	}

	for _, function := range documented.Funcs {
		object := scope.Lookup(function.Name)
		if object == nil {
			continue
		}
		entry.Symbols = append(entry.Symbols, Symbol{
			Kind:      "func",
			Name:      function.Name,
			Signature: types.ObjectString(object, qualifier),
			Doc:       condense(function.Doc),
		})
	}

	for _, declared := range documented.Types {
		object := scope.Lookup(declared.Name)
		if object == nil {
			continue
		}
		symbol := Symbol{
			Kind:      "type",
			Name:      declared.Name,
			Signature: declaration(pkg.Fset, declared),
			Doc:       condense(declared.Doc),
			Fields:    fieldsOf(pkg, declared),
		}
		for _, method := range append(append([]*doc.Func{}, declared.Funcs...), declared.Methods...) {
			symbol.Methods = append(symbol.Methods, Method{
				Name:      method.Name,
				Signature: methodSignature(pkg.Fset, method),
				Doc:       condense(method.Doc),
			})
		}
		sort.Slice(symbol.Methods, func(i, j int) bool {
			return symbol.Methods[i].Name < symbol.Methods[j].Name
		})
		entry.Symbols = append(entry.Symbols, symbol)
	}

	if len(entry.Symbols) == 0 && entry.Doc == "" {
		return nil, nil
	}
	sort.Slice(entry.Symbols, func(i, j int) bool {
		if entry.Symbols[i].Kind != entry.Symbols[j].Kind {
			return kindOrder(entry.Symbols[i].Kind) < kindOrder(entry.Symbols[j].Kind)
		}
		return entry.Symbols[i].Name < entry.Symbols[j].Name
	})
	return entry, nil
}

func fieldsOf(pkg *packages.Package, declared *doc.Type) []Field {
	if declared.Decl == nil {
		return nil
	}
	var fields []Field
	for _, spec := range declared.Decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok || structType.Fields == nil {
			continue
		}
		for _, field := range structType.Fields.List {
			tag := ""
			if field.Tag != nil {
				if unquoted, err := strconv.Unquote(field.Tag.Value); err == nil {
					tag = unquoted
				}
			}
			rendered := render(pkg.Fset, field.Type)
			if len(field.Names) == 0 {
				fields = append(fields, Field{
					Name: embeddedName(rendered),
					Type: rendered,
					Tag:  tag,
					Doc:  condense(field.Doc.Text()),
				})
				continue
			}
			for _, name := range field.Names {
				fields = append(fields, Field{
					Name: name.Name,
					Type: rendered,
					Tag:  tag,
					Doc:  condense(field.Doc.Text()),
				})
			}
		}
	}
	return fields
}

func declaration(fset *token.FileSet, declared *doc.Type) string {
	if declared.Decl == nil {
		return "type " + declared.Name
	}
	copied := *declared.Decl
	copied.Doc = nil
	rendered := render(fset, &copied)
	if index := strings.Index(rendered, "{"); index >= 0 && strings.Contains(rendered, "\n") {
		head := strings.TrimSpace(rendered[:index])
		return head + " { ... }"
	}
	return rendered
}

func methodSignature(fset *token.FileSet, method *doc.Func) string {
	if method.Decl == nil {
		return "func " + method.Name + "()"
	}
	copied := *method.Decl
	copied.Doc = nil
	copied.Body = nil
	return render(fset, &copied)
}

func render(fset *token.FileSet, node any) string {
	var buffer bytes.Buffer
	config := printer.Config{Mode: printer.UseSpaces, Tabwidth: 4}
	if err := config.Fprint(&buffer, fset, node); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(buffer.String()), " ")
}

func embeddedName(rendered string) string {
	trimmed := strings.TrimPrefix(rendered, "*")
	if index := strings.Index(trimmed, "["); index >= 0 {
		trimmed = trimmed[:index]
	}
	if index := strings.LastIndex(trimmed, "."); index >= 0 {
		trimmed = trimmed[index+1:]
	}
	return trimmed
}

func condense(text string) string {
	fields := strings.Fields(strings.ReplaceAll(text, "\n", " "))
	return strings.Join(fields, " ")
}

func kindOrder(kind string) int {
	switch kind {
	case "const":
		return 0
	case "var":
		return 1
	case "type":
		return 2
	default:
		return 3
	}
}

func layerOf(path string) string {
	switch {
	case strings.Contains(path, "/v2/core"):
		return "core"
	case strings.Contains(path, "/v2/app"):
		return "app"
	case strings.Contains(path, "/v2/examples"):
		return "example"
	case strings.HasSuffix(path, "/v2"):
		return "root"
	default:
		return "other"
	}
}

func moduleInfo(dir string) (string, string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", "", fmt.Errorf("catalog: read go.mod: %w", err)
	}
	var module, goVersion string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "module "):
			module = strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
		case strings.HasPrefix(trimmed, "go "):
			goVersion = strings.TrimSpace(strings.TrimPrefix(trimmed, "go "))
		}
	}
	if module == "" {
		return "", "", fmt.Errorf("catalog: no module directive in %s/go.mod", dir)
	}
	return module, goVersion, nil
}

// Marshal renders the catalog as deterministic JSON.
func Marshal(api *API) ([]byte, error) {
	data, err := json.MarshalIndent(api, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("catalog: marshal: %w", err)
	}
	return append(data, '\n'), nil
}

// DocsDir is the embedded documentation directory, relative to the development
// module root.
const DocsDir = "mcp/docs"

// Write regenerates the catalog file and the embedded documentation under the
// development module root.
func Write(ctx context.Context, devRoot, runtimeRoot string) (string, error) {
	api, err := Extract(ctx, runtimeRoot)
	if err != nil {
		return "", err
	}
	data, err := Marshal(api)
	if err != nil {
		return "", err
	}
	path := filepath.Join(devRoot, filepath.FromSlash(File))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("catalog: create directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("catalog: write: %w", err)
	}
	if err := syncDocuments(devRoot, runtimeRoot, false); err != nil {
		return "", err
	}
	return path, nil
}

// documentSources maps an embedded document name to its source file in the
// runtime module. The server ships the real documentation, so a client never
// reads a paraphrase of it.
func documentSources(runtimeRoot string) (map[string]string, error) {
	sources := map[string]string{"readme": filepath.Join(runtimeRoot, "README.md")}
	entries, err := os.ReadDir(filepath.Join(runtimeRoot, "docs"))
	if err != nil {
		return nil, fmt.Errorf("catalog: read docs: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		sources[strings.TrimSuffix(name, ".md")] = filepath.Join(runtimeRoot, "docs", name)
	}
	return sources, nil
}

func syncDocuments(devRoot, runtimeRoot string, checkOnly bool) error {
	sources, err := documentSources(runtimeRoot)
	if err != nil {
		return err
	}
	target := filepath.Join(devRoot, filepath.FromSlash(DocsDir))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("catalog: create %s: %w", target, err)
	}

	expected := make(map[string]struct{}, len(sources))
	for name, source := range sources {
		content, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("catalog: read %s: %w", source, err)
		}
		destination := filepath.Join(target, name+".md")
		expected[name+".md"] = struct{}{}

		if checkOnly {
			current, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(current, content) {
				return fmt.Errorf("catalog: %s is stale, run: foundation catalog", destination)
			}
			continue
		}
		if err := os.WriteFile(destination, content, 0o644); err != nil {
			return fmt.Errorf("catalog: write %s: %w", destination, err)
		}
	}

	present, err := os.ReadDir(target)
	if err != nil {
		return fmt.Errorf("catalog: read %s: %w", target, err)
	}
	for _, entry := range present {
		if _, ok := expected[entry.Name()]; ok {
			continue
		}
		orphan := filepath.Join(target, entry.Name())
		if checkOnly {
			return fmt.Errorf("catalog: %s is no longer a source document, run: foundation catalog", orphan)
		}
		if err := os.Remove(orphan); err != nil {
			return fmt.Errorf("catalog: remove %s: %w", orphan, err)
		}
	}
	return nil
}

// Check reports whether the committed catalog matches the current source.
func Check(ctx context.Context, devRoot, runtimeRoot string) error {
	api, err := Extract(ctx, runtimeRoot)
	if err != nil {
		return err
	}
	want, err := Marshal(api)
	if err != nil {
		return err
	}
	path := filepath.Join(devRoot, filepath.FromSlash(File))
	got, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("catalog: read %s: %w", path, err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("catalog: %s is stale, run: foundation catalog", path)
	}
	return syncDocuments(devRoot, runtimeRoot, true)
}
