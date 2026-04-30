package bind

import (
	"net/http/httptest"
	"testing"
)

func TestBinder_QueryBinding(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?name=alice&age=30", nil)

	type Target struct {
		Name string `query:"name"`
		Age  int    `query:"age"`
	}

	b := New().FromQuery(req)
	var target Target
	if err := b.Bind(&target); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if target.Name != "alice" {
		t.Errorf("Name = %q, want alice", target.Name)
	}
	if target.Age != 30 {
		t.Errorf("Age = %d, want 30", target.Age)
	}
}

func TestBinder_HeaderBinding(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "abc-123")

	type Target struct {
		RequestID string `header:"X-Request-ID"`
	}

	b := New().FromHeader(req)
	var target Target
	if err := b.Bind(&target); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if target.RequestID != "abc-123" {
		t.Errorf("RequestID = %q, want abc-123", target.RequestID)
	}
}

func TestBinder_PathBinding(t *testing.T) {
	params := map[string]string{"id": "42"}

	type Target struct {
		ID int `path:"id"`
	}

	b := New().FromPath(func(key string) string { return params[key] })
	var target Target
	if err := b.Bind(&target); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if target.ID != 42 {
		t.Errorf("ID = %d, want 42", target.ID)
	}
}

func TestBinder_DefaultValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)

	type Target struct {
		Page    int    `query:"page" default:"1"`
		Verbose bool   `query:"verbose" default:"true"`
		Format  string `default:"json"`
	}

	b := New().FromQuery(req)
	var target Target
	if err := b.Bind(&target); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if target.Page != 1 {
		t.Errorf("Page = %d, want 1", target.Page)
	}
	if !target.Verbose {
		t.Error("Verbose = false, want true")
	}
	if target.Format != "json" {
		t.Errorf("Format = %q, want json", target.Format)
	}
}

func TestBinder_QueryOverridesDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?page=5", nil)

	type Target struct {
		Page int `query:"page" default:"1"`
	}

	b := New().FromQuery(req)
	var target Target
	if err := b.Bind(&target); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if target.Page != 5 {
		t.Errorf("Page = %d, want 5", target.Page)
	}
}

func TestBinder_BindJSON(t *testing.T) {
	type Inner struct {
		Name string `json:"Name"`
	}

	type Target struct {
		Body Inner `body:"json"`
	}

	var target Target
	if err := New().BindJSON(&target, []byte(`{"Name":"bob"}`)); err != nil {
		t.Fatalf("BindJSON: %v", err)
	}
	if target.Body.Name != "bob" {
		t.Errorf("Name = %q, want bob", target.Body.Name)
	}
}

func TestBinder_AllSources(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?search=golang", nil)
	req.Header.Set("X-Token", "secret")
	params := map[string]string{"id": "99"}

	type Target struct {
		ID     int    `path:"id"`
		Search string `query:"search"`
		Token  string `header:"X-Token"`
		Limit  int    `default:"10"`
	}

	b := New().
		FromPath(func(key string) string { return params[key] }).
		FromQuery(req).
		FromHeader(req)

	var target Target
	if err := b.Bind(&target); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if target.ID != 99 {
		t.Errorf("ID = %d, want 99", target.ID)
	}
	if target.Search != "golang" {
		t.Errorf("Search = %q, want golang", target.Search)
	}
	if target.Token != "secret" {
		t.Errorf("Token = %q, want secret", target.Token)
	}
	if target.Limit != 10 {
		t.Errorf("Limit = %d, want 10", target.Limit)
	}
}