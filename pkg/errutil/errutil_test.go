package errutil

import (
	"errors"
	"strings"
	"testing"
)

func TestMultiError_Append(t *testing.T) {
	e := &MultiError{}
	e.Append(errors.New("err1"))
	e.Append(nil)
	e.Append(errors.New("err2"))

	if len(e.Errors) != 2 {
		t.Errorf("got %d errors, want 2", len(e.Errors))
	}
}

func TestMultiError_Error(t *testing.T) {
	e := &MultiError{}
	if e.Error() != "" {
		t.Errorf("empty multierror should return empty string, got %q", e.Error())
	}

	e.Append(errors.New("one"))
	if e.Error() != "one" {
		t.Errorf("single error: got %q, want %q", e.Error(), "one")
	}

	e.Append(errors.New("two"))
	want := "multiple errors occurred: one; two"
	if e.Error() != want {
		t.Errorf("multiple errors: got %q, want %q", e.Error(), want)
	}
}

func TestMultiError_Unwrap(t *testing.T) {
	err1 := errors.New("err1")
	err2 := errors.New("err2")
	e := &MultiError{Errors: []error{err1, err2}}

	unwrapped := e.Unwrap()
	if len(unwrapped) != 2 || unwrapped[0] != err1 || unwrapped[1] != err2 {
		t.Error("Unwrap should return all errors")
	}
}

func TestJoinErrors(t *testing.T) {
	err := JoinErrors(errors.New("a"), nil, errors.New("b"))
	if err == nil {
		t.Fatal("JoinErrors should return error")
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Errorf("JoinErrors error content: %v", err)
	}

	if JoinErrors(nil, nil) != nil {
		t.Error("JoinErrors with only nil should return nil")
	}
}