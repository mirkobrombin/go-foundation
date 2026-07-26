package di

import "reflect"

// Key is a typed name for a dependency.
type Key[T any] struct {
	name string
}

// NewKey creates a typed dependency key.
func NewKey[T any](name string) Key[T] {
	if name == "" {
		panic("di: dependency name cannot be empty")
	}
	return Key[T]{name: name}
}

// Name returns the dependency name.
func (k Key[T]) Name() string {
	return k.name
}

// ProvideKey registers a typed named dependency.
func ProvideKey[T any](b *Builder, key Key[T], instance T) {
	b.Provide(key.name, instance)
}

// ResolveKey retrieves a dependency through a typed key.
func ResolveKey[T any](c *Container, key Key[T]) (T, bool) {
	return Resolve[T](c, key.name)
}

// MustResolveKey retrieves a dependency through a typed key and panics when missing.
func MustResolveKey[T any](c *Container, key Key[T]) T {
	return MustResolve[T](c, key.name)
}

// ProvideLazyKey registers a typed lazy dependency.
func ProvideLazyKey[T any](c *Container, key Key[T], factory func() T) {
	if factory == nil {
		panic("di: lazy dependency factory cannot be nil")
	}
	c.ProvideLazy(key.name, func() any {
		return factory()
	}, reflect.TypeOf((*T)(nil)).Elem())
}
