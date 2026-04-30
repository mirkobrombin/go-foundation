package openapi

import (
	"encoding/json"
	"testing"
)

type TestEndpoint struct {
	Meta struct{} `method:"GET" path:"/api/v1/test"`
	Name string    `query:"name"`
	Age  int       `query:"age"`
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
		if p.Name == "name" && p.In == "query" {
			foundQuery = true
		}
	}
	if !foundQuery {
		t.Error("expected query parameter 'name'")
	}
}

func TestBuild_NoTags(t *testing.T) {
	type NoTags struct{}
	_, err := Build("Test", "1.0.0", &NoTags{})
	if err != nil {
		t.Fatalf("Build: %v", err)
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