package validation

import (
	"testing"
)

type user struct {
	Name  string `validate:"required"`
	Age   int    `validate:"min=18,max=99"`
	Email string `validate:"email"`
	Label string `validate:"pattern=^[a-z]+$"`
}

type optionalInt struct {
	Count int `validate:"required"`
}

func TestValidator_RequiredString(t *testing.T) {
	v := New()
	errs := v.Validate(user{Name: "", Email: "a@b.c"})
	if len(errs) == 0 {
		t.Error("expected error for empty Name")
	}
	found := false
	for _, e := range errs {
		if e.Field == "Name" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error on field Name")
	}
}

func TestValidator_MinInt(t *testing.T) {
	v := New()
	errs := v.Validate(user{Name: "x", Age: 10, Email: "a@b.c"})
	found := false
	for _, e := range errs {
		if e.Field == "Age" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error for Age < 18")
	}
}

func TestValidator_MaxInt(t *testing.T) {
	v := New()
	errs := v.Validate(user{Name: "x", Age: 100, Email: "a@b.c"})
	found := false
	for _, e := range errs {
		if e.Field == "Age" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error for Age > 99")
	}
}

func TestValidator_ValidEmail(t *testing.T) {
	v := New()
	errs := v.Validate(user{Name: "x", Age: 25, Email: "notanemail"})
	found := false
	for _, e := range errs {
		if e.Field == "Email" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error for invalid email")
	}
}

func TestValidator_Pattern(t *testing.T) {
	v := New()
	errs := v.Validate(user{Name: "x", Age: 25, Email: "a@b.c", Label: "ABC"})
	found := false
	for _, e := range errs {
		if e.Field == "Label" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error for Label pattern mismatch")
	}
}

func TestValidator_AllValid(t *testing.T) {
	v := New()
	errs := v.Validate(user{Name: "Alice", Age: 25, Email: "alice@example.com", Label: "abc"})
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidator_EmptyRequired(t *testing.T) {
	v := New()
	errs := v.Validate(user{Name: "", Age: 25, Email: "a@b.c"})
	if len(errs) == 0 {
		t.Error("expected error for required field")
	}
}

func TestValidator_NonStruct(t *testing.T) {
	v := New()
	errs := v.Validate(42)
	if len(errs) == 0 {
		t.Error("expected error for non-struct")
	}
}

func TestValidator_ErrorsType_ErrorMethod(t *testing.T) {
	errs := Errors{{Field: "x", Message: "bad"}}
	if errs.Error() != "x: bad" {
		t.Errorf("got %q", errs.Error())
	}
}

func TestValidator_OptionalInt(t *testing.T) {
	v := New()
	errs := v.Validate(optionalInt{Count: 0})
	if len(errs) > 0 {
		t.Error("zero int should be allowed for required since Go can't distinguish zero from unset")
	}
}
