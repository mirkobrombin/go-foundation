package validation

import (
	"reflect"
	"strings"
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

func TestValidatorRejectsUnknownRule(t *testing.T) {
	type target struct {
		Value string `validate:"misspelled"`
	}
	errs := New().Validate(target{Value: "anything"})
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "unknown validation rule") {
		t.Fatalf("Validate() = %v, want unknown rule error", errs)
	}
}

func TestValidatorRejectsInvalidRuleParameters(t *testing.T) {
	type target struct {
		Count int    `validate:"min=wrong"`
		Value string `validate:"pattern=["`
	}
	errs := New().Validate(target{})
	if len(errs) != 2 {
		t.Fatalf("Validate() errors = %v, want two configuration errors", errs)
	}
}

func TestRegisterInvalidatesCompiledTypes(t *testing.T) {
	type target struct {
		Value string `validate:"custom"`
	}
	validator := New()
	if len(validator.Validate(target{})) != 1 {
		t.Fatal("Validate() did not reject unknown rule")
	}
	if err := validator.Register("custom", func(reflect.Value, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if errs := validator.Validate(target{}); len(errs) != 0 {
		t.Fatalf("Validate() retained stale unknown rule: %v", errs)
	}
}
