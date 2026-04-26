package di

import (
	"reflect"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/contracts"
	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// Container manages dependency injection with thread-safe access.
//
// Example:
//
//	c := di.New()
//	c.Provide("db", &Database{})
//	db := di.Get[*Database](c, "db")
type Container struct {
	providers map[string]any
	mu        sync.RWMutex
}

var injectParser = tags.NewParser("inject", tags.WithPairDelimiter(";"), tags.WithKVSeparator(":"), tags.WithIncludeUntagged())

// New creates an empty DI container.
func New() *Container {
	return &Container{
		providers: make(map[string]any),
	}
}

// Provide registers a dependency by name.
// It automatically verifies any contracts declared via contracts.Implements.
func (c *Container) Provide(name string, instance any) {
	contracts.MustVerify(instance)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers[name] = instance
}

// Get retrieves a dependency by name.
// If the provider is lazy, the factory is called on first access.
//
// Returns:
//
// The value and true if found, otherwise nil and false.
func (c *Container) Get(name string) (any, bool) {
	c.mu.RLock()
	v, ok := c.providers[name]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if lp, ok := v.(*lazyProvider); ok {
		return lp.get(), true
	}
	return v, true
}

// ResolveAll finds all registered dependencies that implement the given interface T.
func ResolveAll[T any](c *Container) []T {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []T
	for _, v := range c.providers {
		if t, ok := v.(T); ok {
			result = append(result, t)
		}
	}
	return result
}

// MustGet retrieves a dependency and panics if not found.
//
// Notes:
//
// Panics if the dependency is missing.
func (c *Container) MustGet(name string) any {
	v, ok := c.Get(name)
	if !ok {
		panic("di: dependency not found: " + name)
	}
	return v
}

// Has checks if a dependency is registered.
func (c *Container) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.providers[name]
	return ok
}

// Inject populates struct fields with registered dependencies.
// Uses `inject:"name"` tags to match fields with providers.
// Falls back to field name if no inject tag is present.
func (c *Container) Inject(target any) {
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return
	}

	elem := val.Elem()
	parser := injectParser
	fields := parser.ParseStruct(target)

	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, meta := range fields {
		fieldVal := elem.Field(meta.Index)
		if !fieldVal.CanSet() {
			continue
		}

		name := meta.RawTag
		if name == "" {
			name = meta.Name
		}

		if dep, ok := c.providers[name]; ok {
			if lp, ok := dep.(*lazyProvider); ok {
				dep = lp.get()
			}
			depVal := reflect.ValueOf(dep)
			if depVal.Type().AssignableTo(fieldVal.Type()) {
				fieldVal.Set(depVal)
			}
		}
	}
}

// Scope creates a child container that inherits from parent.
// The child can override providers without affecting the parent.
func (c *Container) Scope() *Container {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := New()
	for k, v := range c.providers {
		clone.providers[k] = v
	}
	return clone
}

// ProvideLazy registers a factory that is called only on first Get.
func (c *Container) ProvideLazy(name string, factory func() any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers[name] = &lazyProvider{factory: factory}
}

type lazyProvider struct {
	once    sync.Once
	value   any
	factory func() any
}

func (l *lazyProvider) get() any {
	l.once.Do(func() {
		l.value = l.factory()
	})
	return l.value
}

// Clone creates a shallow copy of the container.
func (c *Container) Clone() *Container {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := New()
	for k, v := range c.providers {
		clone.providers[k] = v
	}
	return clone
}

// Keys returns all registered dependency names.
func (c *Container) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.providers))
	for k := range c.providers {
		keys = append(keys, k)
	}
	return keys
}
