package di

// Resolve retrieves a typed dependency from the container by name.
func Resolve[T any](c *Container, name string) (T, bool) {
	var zero T
	v, ok := c.Get(name)
	if !ok {
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

// MustResolve retrieves a typed dependency by name and panics if not found.
func MustResolve[T any](c *Container, name string) T {
	v, ok := Resolve[T](c, name)
	if !ok {
		panic("di: cannot resolve " + name)
	}
	return v
}

// ResolveAll finds all named dependencies that implement the given interface T.
func ResolveAll[T any](c *Container) []T {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []T
	for _, v := range c.named {
		if lp, ok := v.(*lazyProvider); ok {
			v = lp.get()
		}
		if t, ok := v.(T); ok {
			result = append(result, t)
		}
	}
	return result
}