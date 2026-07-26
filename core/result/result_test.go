package result

import (
	"errors"
	"strconv"
	"testing"
)

func TestResult_Ok(t *testing.T) {
	r := Ok(42)

	if !r.IsOk() {
		t.Error("IsOk should be true")
	}
	if r.IsErr() {
		t.Error("IsErr should be false")
	}
	if r.Unwrap() != 42 {
		t.Errorf("Unwrap: got %d, want 42", r.Unwrap())
	}
}

func TestResult_Err(t *testing.T) {
	r := Err[int](errors.New("failed"))

	if r.IsOk() {
		t.Error("IsOk should be false")
	}
	if !r.IsErr() {
		t.Error("IsErr should be true")
	}
	if r.Error() == nil {
		t.Error("Error should not be nil")
	}
}

func TestResult_Unwrap_Panic(t *testing.T) {
	r := Err[int](errors.New("panic test"))

	defer func() {
		if recover() == nil {
			t.Error("Unwrap should panic on error")
		}
	}()

	r.Unwrap()
}

func TestResult_UnwrapOr(t *testing.T) {
	ok := Ok(10)
	err := Err[int](errors.New("fail"))

	if ok.UnwrapOr(0) != 10 {
		t.Error("UnwrapOr should return value for Ok")
	}
	if err.UnwrapOr(99) != 99 {
		t.Error("UnwrapOr should return default for Err")
	}
}

func TestResult_UnwrapOrElse(t *testing.T) {
	r := Err[int](errors.New("custom"))

	val := r.UnwrapOrElse(func(e error) int {
		return len(e.Error())
	})

	if val != 6 {
		t.Errorf("UnwrapOrElse: got %d, want 6", val)
	}
}

func TestResult_Value(t *testing.T) {
	r := Ok("hello")
	v, err := r.Value()

	if err != nil {
		t.Error("err should be nil")
	}
	if v != "hello" {
		t.Errorf("value: got %q, want %q", v, "hello")
	}
}

func TestMap(t *testing.T) {
	r := Ok(5)
	doubled := Map(r, func(n int) int { return n * 2 })

	if doubled.Unwrap() != 10 {
		t.Errorf("Map: got %d, want 10", doubled.Unwrap())
	}
}

func TestMap_PropagatesError(t *testing.T) {
	r := Err[int](errors.New("original"))
	mapped := Map(r, func(n int) string { return "never" })

	if mapped.IsOk() {
		t.Error("Map should propagate error")
	}
}

func TestFlatMap(t *testing.T) {
	r := Ok("123")
	parsed := FlatMap(r, func(s string) Result[int] {
		n, err := strconv.Atoi(s)
		return From(n, err)
	})

	if parsed.Unwrap() != 123 {
		t.Errorf("FlatMap: got %d, want 123", parsed.Unwrap())
	}
}

func TestFrom(t *testing.T) {
	r1 := From(42, nil)
	if !r1.IsOk() {
		t.Error("From with nil error should be Ok")
	}

	r2 := From(0, errors.New("fail"))
	if !r2.IsErr() {
		t.Error("From with error should be Err")
	}
}

func TestTry(t *testing.T) {
	r := Try(func() (int, error) {
		return 100, nil
	})

	if r.Unwrap() != 100 {
		t.Errorf("Try: got %d, want 100", r.Unwrap())
	}
}

func TestMust(t *testing.T) {
	r := Ok("success")
	if Must(r) != "success" {
		t.Error("Must should return value")
	}
}
