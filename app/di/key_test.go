package di

import "testing"

func TestKey(t *testing.T) {
	key := NewKey[int]("answer")
	b := NewBuilder()
	ProvideKey(b, key, 42)

	c := b.MustBuild()
	got, ok := ResolveKey(c, key)
	if !ok || got != 42 {
		t.Fatalf("ResolveKey() = %v, %v", got, ok)
	}
}

func TestKeyRejectsWrongType(t *testing.T) {
	key := NewKey[int]("answer")
	c := New()
	c.Provide(key.Name(), "wrong")

	if _, ok := ResolveKey(c, key); ok {
		t.Fatal("ResolveKey() accepted the wrong type")
	}
}

func TestProvideLazyKeyRejectsNilFactory(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ProvideLazyKey() did not panic")
		}
	}()
	ProvideLazyKey(New(), NewKey[int]("answer"), nil)
}

func TestProvideLazyKeyCanBeValidatedWithoutInstantiation(t *testing.T) {
	type target struct {
		Answer int `inject:"answer"`
	}
	container := New()
	called := false
	ProvideLazyKey(container, NewKey[int]("answer"), func() int {
		called = true
		return 42
	})
	if err := container.ValidateTarget(&target{}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("ValidateTarget() instantiated a lazy dependency")
	}
}
