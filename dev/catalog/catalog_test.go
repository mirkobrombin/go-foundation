package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractDescribesTheRuntimeModule checks the extraction against types that
// must exist. If the catalog silently stopped seeing methods or tags, every
// answer the MCP server gives would degrade without any test failing, so this
// asserts the shape and not only the count.
func TestExtractDescribesTheRuntimeModule(t *testing.T) {
	api, err := Extract(context.Background(), filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if api.Module != "github.com/mirkobrombin/go-foundation/v2" {
		t.Fatalf("Module = %q", api.Module)
	}
	if len(api.Packages) < 40 {
		t.Fatalf("packages = %d, the extraction looks truncated", len(api.Packages))
	}

	var web, appPkg *Package
	for index := range api.Packages {
		switch api.Packages[index].Path {
		case "github.com/mirkobrombin/go-foundation/v2/app/web":
			web = &api.Packages[index]
		case "github.com/mirkobrombin/go-foundation/v2/app":
			appPkg = &api.Packages[index]
		}
	}
	if web == nil || appPkg == nil {
		t.Fatal("app or app/web is missing from the catalog")
	}
	if web.Layer != "app" {
		t.Fatalf("app/web layer = %q", web.Layer)
	}

	definition := findSymbol(web, "HandlerDefinition")
	if definition == nil {
		t.Fatal("web.HandlerDefinition is missing")
	}
	if len(definition.Fields) == 0 {
		t.Fatal("web.HandlerDefinition has no fields, struct extraction is broken")
	}

	application := findSymbol(appPkg, "App")
	if application == nil {
		t.Fatal("app.App is missing")
	}
	if len(application.Methods) == 0 {
		t.Fatal("app.App has no methods, method extraction is broken")
	}
	var registerHTTP *Method
	for index := range application.Methods {
		if application.Methods[index].Name == "RegisterHTTP" {
			registerHTTP = &application.Methods[index]
		}
	}
	if registerHTTP == nil {
		t.Fatal("app.App.RegisterHTTP is missing")
	}
	if !strings.Contains(registerHTTP.Signature, "func (a *App) RegisterHTTP") {
		t.Fatalf("RegisterHTTP signature = %q", registerHTTP.Signature)
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	api, err := Extract(context.Background(), filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	first, err := Marshal(api)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	second, err := Marshal(api)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("Marshal is not deterministic")
	}
}

func TestCommittedCatalogMatchesTheSource(t *testing.T) {
	if err := Check(context.Background(), "..", filepath.Join("..", "..")); err != nil {
		t.Fatalf("the committed catalog is stale: %v", err)
	}
}

func findSymbol(pkg *Package, name string) *Symbol {
	for index := range pkg.Symbols {
		if pkg.Symbols[index].Name == name {
			return &pkg.Symbols[index]
		}
	}
	return nil
}
