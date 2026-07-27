// Package mcp serves Foundation knowledge and Foundation tooling over the
// Model Context Protocol.
//
// The server exists because a language model asked to write Foundation code has
// two ways to be wrong: it can invent an API that does not exist, and it can
// invent a workflow that skips the checks. The tools here remove the first by
// answering from an API catalog extracted from the source, and the second by
// running the real analyzer, generator, compiler, and tests, and returning what
// they actually said.
//
// Every answer is derived from the version of Foundation the binary was built
// from. Nothing here is written from memory.
package mcp

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/mirkobrombin/go-foundation/dev/v2/catalog"
)

//go:embed api.json
var catalogData []byte

//go:embed docs/*.md
var documents embed.FS

var (
	loadOnce sync.Once
	api      *catalog.API
	loadErr  error
)

// API returns the embedded catalog of the Foundation runtime module.
func API() (*catalog.API, error) {
	loadOnce.Do(func() {
		api = &catalog.API{}
		if err := json.Unmarshal(catalogData, api); err != nil {
			loadErr = fmt.Errorf("mcp: decode api catalog: %w", err)
		}
	})
	return api, loadErr
}

// PackageByPath returns one package of the catalog. The lookup accepts the full
// import path, the path without the module prefix, or the trailing directory,
// because a model rarely holds the full path and guessing is what we are trying
// to avoid.
func PackageByPath(name string) (*catalog.Package, error) {
	loaded, err := API()
	if err != nil {
		return nil, err
	}
	wanted := strings.TrimSuffix(strings.TrimSpace(name), "/")
	if wanted == "" {
		return nil, fmt.Errorf("mcp: empty package name")
	}
	for _, pkg := range loaded.Packages {
		if pkg.Path == wanted {
			return &pkg, nil
		}
	}
	for _, pkg := range loaded.Packages {
		trimmed := strings.TrimPrefix(pkg.Path, loaded.Module+"/")
		if trimmed == wanted || pkg.Name == wanted || strings.HasSuffix(pkg.Path, "/"+wanted) {
			return &pkg, nil
		}
	}
	return nil, fmt.Errorf("mcp: unknown package %q, call foundation_packages for the list", name)
}

// SymbolMatch is one hit of a catalog search.
type SymbolMatch struct {
	Package   string `json:"package"`
	Layer     string `json:"layer"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Doc       string `json:"doc,omitempty"`
	Method    string `json:"method,omitempty"`
}

// SearchSymbols finds exported declarations by name. Exact matches come first,
// then prefix matches, then substring matches, so the obvious answer is not
// buried under near misses.
func SearchSymbols(query string, limit int) ([]SymbolMatch, error) {
	loaded, err := API()
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, fmt.Errorf("mcp: empty query")
	}
	if limit <= 0 {
		limit = 40
	}

	type ranked struct {
		match SymbolMatch
		score int
	}
	var found []ranked
	consider := func(match SymbolMatch, name string) {
		lowered := strings.ToLower(name)
		switch {
		case lowered == needle:
			found = append(found, ranked{match, 0})
		case strings.HasPrefix(lowered, needle):
			found = append(found, ranked{match, 1})
		case strings.Contains(lowered, needle):
			found = append(found, ranked{match, 2})
		}
	}

	for _, pkg := range loaded.Packages {
		for _, symbol := range pkg.Symbols {
			consider(SymbolMatch{
				Package:   pkg.Path,
				Layer:     pkg.Layer,
				Kind:      symbol.Kind,
				Name:      symbol.Name,
				Signature: symbol.Signature,
				Doc:       symbol.Doc,
			}, symbol.Name)
			for _, method := range symbol.Methods {
				consider(SymbolMatch{
					Package:   pkg.Path,
					Layer:     pkg.Layer,
					Kind:      "method",
					Name:      symbol.Name + "." + method.Name,
					Signature: method.Signature,
					Doc:       method.Doc,
					Method:    method.Name,
				}, method.Name)
			}
		}
	}

	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score < found[j].score
		}
		if found[i].match.Package != found[j].match.Package {
			return found[i].match.Package < found[j].match.Package
		}
		return found[i].match.Name < found[j].match.Name
	})

	matches := make([]SymbolMatch, 0, limit)
	for _, entry := range found {
		if len(matches) == limit {
			break
		}
		matches = append(matches, entry.match)
	}
	return matches, nil
}

// Document returns an embedded documentation file by name, with or without the
// .md suffix.
func Document(name string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(name), ".md")
	if trimmed == "" {
		return "", fmt.Errorf("mcp: empty document name")
	}
	data, err := documents.ReadFile("docs/" + trimmed + ".md")
	if err != nil {
		available, _ := DocumentNames()
		return "", fmt.Errorf("mcp: unknown document %q, available: %s", name, strings.Join(available, ", "))
	}
	return string(data), nil
}

// DocumentNames lists the embedded documentation files.
func DocumentNames() ([]string, error) {
	entries, err := fs.ReadDir(documents, "docs")
	if err != nil {
		return nil, fmt.Errorf("mcp: read documents: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, strings.TrimSuffix(entry.Name(), ".md"))
	}
	sort.Strings(names)
	return names, nil
}

// SymbolCount reports how many exported declarations the catalog holds, methods
// included. It is used to prove the catalog was loaded rather than assumed.
func SymbolCount() (int, error) {
	loaded, err := API()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, pkg := range loaded.Packages {
		total += len(pkg.Symbols)
		for _, symbol := range pkg.Symbols {
			total += len(symbol.Methods)
		}
	}
	return total, nil
}
