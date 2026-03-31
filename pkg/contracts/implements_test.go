package contracts

import (
	"testing"
)

type Greeter interface {
	Greet() string
}

type MyGreeter struct {
	Implements[Greeter]
}

func (m *MyGreeter) Greet() string {
	return "Hello"
}

type BadGreeter struct {
	Implements[Greeter]
}

func TestVerify(t *testing.T) {
	t.Run("Valid implementation", func(t *testing.T) {
		g := &MyGreeter{}
		if err := Verify(g); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("Invalid implementation", func(t *testing.T) {
		g := &BadGreeter{}
		err := Verify(g)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expected := "contract violation: *contracts.BadGreeter does not implement contracts.Greeter"
		if err.Error() != expected {
			t.Fatalf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("Non-struct type", func(t *testing.T) {
		if err := Verify(42); err != nil {
			t.Fatalf("expected no error for non-struct, got %v", err)
		}
	})
}
