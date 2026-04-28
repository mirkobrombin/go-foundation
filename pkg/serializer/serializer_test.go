package serializer

import (
	"strings"
	"testing"
)

type sample struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Age       int    `json:"age"`
}

func TestPolicy_Marshal_SnakeCase(t *testing.T) {
	p := New(WithNaming(SnakeCase), WithTagName("json"))
	s := sample{FirstName: "John", LastName: "Doe", Age: 30}

	data, err := p.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, `"first_name":"John"`) || !strings.Contains(got, `"last_name":"Doe"`) || !strings.Contains(got, `"age":30`) {
		t.Errorf("Marshal: got %q", got)
	}
}

func TestPolicy_Unmarshal_SnakeCase(t *testing.T) {
	p := New(WithNaming(SnakeCase), WithTagName("json"))
	data := []byte(`{"first_name":"Jane","last_name":"Smith","age":25}`)

	var s sample
	if err := p.Unmarshal(data, &s); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if s.FirstName != "Jane" {
		t.Errorf("FirstName: got %q, want %q", s.FirstName, "Jane")
	}
	if s.Age != 25 {
		t.Errorf("Age: got %d, want %d", s.Age, 25)
	}
}

func TestPolicy_IgnoreNil(t *testing.T) {
	p := New(WithIgnoreNil(), WithTagName("json"))
	type withPtr struct {
		Name  string  `json:"name"`
		Score *int    `json:"score"`
	}
	s := withPtr{Name: "test", Score: nil}
	data, err := p.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	got := string(data)
	if got != `{"name":"test"}` {
		t.Errorf("Marshal with IgnoreNil: got %q", got)
	}
}

func TestPolicy_IgnoreZero(t *testing.T) {
	p := New(WithIgnoreZero(), WithTagName("json"))
	type withZero struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	s := withZero{Name: "test", Age: 0}
	data, err := p.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	got := string(data)
	if got != `{"name":"test"}` {
		t.Errorf("Marshal with IgnoreZero: got %q", got)
	}
}

func TestPolicy_CamelCase(t *testing.T) {
	p := New(WithNaming(CamelCase), WithTagName("json"))
	type camel struct {
		FirstName string
		LastName  string
	}
	s := camel{FirstName: "A", LastName: "B"}
	data, err := p.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	got := string(data)
	if got != `{"firstName":"A","lastName":"B"}` {
		t.Errorf("Marshal CamelCase: got %q", got)
	}
}