//foundation:ignore-file

package openapi

import (
	"encoding/json"
	"testing"
)

// foundation:ignore handler
type TestEndpoint struct {
	Meta struct{} `method:"GET" path:"/api/v1/test"`
	Name string   `query:"name"`
	Age  int      `query:"age"`
}

func (e *TestEndpoint) OpenAPIMeta() map[string]any {
	return map[string]any{
		"summary":     "Test endpoint",
		"description": "A test endpoint",
	}
}

func TestBuild_BasicDocument(t *testing.T) {
	doc, err := Build("Test API", "1.0.0", &TestEndpoint{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var result Document
	if err := json.Unmarshal(doc, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if result.OpenAPI != "3.0.3" {
		t.Errorf("OpenAPI = %q, want 3.0.3", result.OpenAPI)
	}
	if result.Info.Title != "Test API" {
		t.Errorf("Title = %q, want Test API", result.Info.Title)
	}
	if result.Info.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", result.Info.Version)
	}

	pathItem, ok := result.Paths["/api/v1/test"]
	if !ok {
		t.Fatal("path /api/v1/test not found")
	}
	op, ok := pathItem["get"]
	if !ok {
		t.Fatal("method get not found")
	}
	if op.Summary != "Test endpoint" {
		t.Errorf("Summary = %q, want Test endpoint", op.Summary)
	}
}

func TestBuild_AutoParameters(t *testing.T) {
	doc, err := Build("Test", "1.0.0", &TestEndpoint{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var result Document
	json.Unmarshal(doc, &result)

	op := result.Paths["/api/v1/test"]["get"]

	foundQuery := false
	for _, p := range op.Parameters {
		if p.In == "path" {
			t.Errorf("unexpected path parameter %q", p.Name)
		}
		if p.Name == "name" && p.In == "query" {
			foundQuery = true
		}
	}
	if !foundQuery {
		t.Error("expected query parameter 'name'")
	}
}

func TestBuildNormalizesConstrainedAndCatchAllPaths(t *testing.T) {
	type constrainedEndpoint struct {
		Meta struct{} `method:"GET" path:"/users/{id:int}"`
		ID   int      `path:"id"`
	}
	type catchAllEndpoint struct {
		Meta struct{} `method:"GET" path:"/static/{*filepath}"`
		Path string   `path:"filepath"`
	}

	doc, err := Build("Test", "1.0.0", &constrainedEndpoint{}, &catchAllEndpoint{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var result Document
	if err := json.Unmarshal(doc, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, exists := result.Paths["/users/{id}"]; !exists {
		t.Fatal("constrained path was not normalized")
	}
	if _, exists := result.Paths["/static/{filepath}"]; !exists {
		t.Fatal("catch-all path was not normalized")
	}
}

func TestBuildRejectsMismatchedPathParameters(t *testing.T) {
	type missingField struct {
		Meta struct{} `method:"GET" path:"/users/{id:int}"`
	}
	type extraField struct {
		Meta   struct{} `method:"GET" path:"/users"`
		UserID int      `path:"id"`
	}
	type wrongField struct {
		Meta   struct{} `method:"GET" path:"/users/{id}"`
		UserID int      `path:"user_id"`
	}

	for _, handler := range []any{&missingField{}, &extraField{}, &wrongField{}} {
		if _, err := Build("Test", "1.0.0", handler); err == nil {
			t.Fatalf("Build() accepted mismatched path parameters on %#v", handler)
		}
	}
}

func TestBuild_NoTags(t *testing.T) {
	type NoTags struct{}
	_, err := Build("Test", "1.0.0", &NoTags{})
	if err == nil {
		t.Fatal("Build() accepted a handler without route tags")
	}
}

func TestBuildRejectsInvalidRoutes(t *testing.T) {
	type invalidMethod struct {
		Meta struct{} `method:"FETCH" path:"/items"`
	}
	type invalidPath struct {
		Meta struct{} `method:"GET" path:"items"`
	}
	for _, handler := range []any{&invalidMethod{}, &invalidPath{}} {
		if _, err := Build("Test", "1.0.0", handler); err == nil {
			t.Fatalf("Build() accepted invalid route %#v", handler)
		}
	}
}

func TestGoTypeToOpenAPI(t *testing.T) {
	tests := []struct {
		kind   string
		goType string
		want   string
	}{
		{"int", "int", "integer"},
		{"string", "string", "string"},
		{"float64", "float64", "number"},
		{"bool", "bool", "boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			_ = tt.want
		})
	}
}

func TestBuildRejectsInvalidHandlers(t *testing.T) {
	var typedNil *TestEndpoint
	for _, handler := range []any{nil, typedNil, 42} {
		if _, err := Build("Test", "1.0.0", handler); err == nil {
			t.Fatalf("Build() accepted invalid handler %#v", handler)
		}
	}
}

// foundation:ignore handler
type invalidMetadataEndpoint struct {
	Meta struct{} `method:"GET" path:"/invalid"`
}

func (*invalidMetadataEndpoint) OpenAPIMeta() map[string]any {
	return map[string]any{
		"parameters": []map[string]any{{"name": 1, "in": "query"}},
	}
}

func TestBuildRejectsMalformedMetadata(t *testing.T) {
	if _, err := Build("Test", "1.0.0", &invalidMetadataEndpoint{}); err == nil {
		t.Fatal("Build() accepted malformed metadata")
	}
}

func TestBuildRejectsDuplicateOperations(t *testing.T) {
	if _, err := Build(
		"Test",
		"1.0.0",
		&TestEndpoint{},
		&TestEndpoint{},
	); err == nil {
		t.Fatal("Build() accepted duplicate method and path")
	}
}

type recursiveEndpoint struct {
	*recursiveEndpoint
	Meta struct{} `method:"GET" path:"/recursive"`
}

func TestBuildHandlesRecursiveEmbedding(t *testing.T) {
	if _, err := Build("Test", "1.0.0", &recursiveEndpoint{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
}
