package di

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/contracts"
	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// Lifetime controls how a service is instantiated.
type Lifetime int

const (
	Transient Lifetime = iota
	Singleton
	Scoped
)

type serviceEntry struct {
	lifetime Lifetime
	factory  any
	instance any
	built    bool
}

// Container holds registered services and named instances.
type Container struct {
	services map[reflect.Type]*serviceEntry
	named    map[string]any
	mu       sync.RWMutex
	parent   *Container
}

var injectParser = tags.NewParser("inject", tags.WithPairDelimiter(";"), tags.WithKVSeparator(":"), tags.WithIncludeUntagged())

// New creates an empty Container.
func New() *Container {
	return &Container{
		services: make(map[reflect.Type]*serviceEntry),
		named:    make(map[string]any),
	}
}

// Builder registers services before building a Container.
type Builder struct {
	services map[reflect.Type]*serviceEntry
	named    map[string]any
	mu       sync.RWMutex
}

// NewBuilder creates an empty Builder.
func NewBuilder() *Builder {
	return &Builder{
		services: make(map[reflect.Type]*serviceEntry),
		named:    make(map[string]any),
	}
}

// Register adds a typed factory to the builder.
func Register[T any](b *Builder, factory func() T, lifetime ...Lifetime) {
	lt := Singleton
	if len(lifetime) > 0 {
		lt = lifetime[0]
	}
	typ := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	b.services[typ] = &serviceEntry{lifetime: lt, factory: factory}
	b.mu.Unlock()
}

// Build creates a Container from the registered services.
func (b *Builder) Build() *Container {
	c := New()
	b.mu.RLock()
	for k, v := range b.services {
		c.services[k] = v
	}
	for k, v := range b.named {
		c.named[k] = v
	}
	b.mu.RUnlock()
	return c
}

// Provide adds a named instance to the builder.
func (b *Builder) Provide(name string, instance any) {
	contracts.MustVerify(instance)
	b.mu.Lock()
	b.named[name] = instance
	b.mu.Unlock()
}

// ResolveType resolves a typed service from the container.
func ResolveType[T any](c *Container) T {
	var zero T
	typ := reflect.TypeOf((*T)(nil)).Elem()

	c.mu.RLock()
	entry, ok := c.services[typ]
	c.mu.RUnlock()

	if !ok {
		if c.parent != nil {
			return ResolveType[T](c.parent)
		}
		panic(fmt.Sprintf("di: cannot resolve type %v", typ))
	}

	switch entry.lifetime {
	case Singleton:
		if entry.built {
			return entry.instance.(T)
		}
		c.mu.Lock()
		if entry.built {
			c.mu.Unlock()
			return entry.instance.(T)
		}
		fn := entry.factory.(func() T)
		instance := fn()
		entry.instance = instance
		entry.built = true
		c.mu.Unlock()
		return instance
	case Transient:
		fn := entry.factory.(func() T)
		return fn()
	case Scoped:
		if entry.built {
			return entry.instance.(T)
		}
		c.mu.Lock()
		if entry.built {
			c.mu.Unlock()
			return entry.instance.(T)
		}
		fn := entry.factory.(func() T)
		instance := fn()
		entry.instance = instance
		entry.built = true
		c.mu.Unlock()
		return instance
	default:
		return zero
	}
}

// Provide adds a named instance to the container.
func (c *Container) Provide(name string, instance any) {
	contracts.MustVerify(instance)
	c.mu.Lock()
	c.named[name] = instance
	c.mu.Unlock()
}

// Get retrieves a named instance from the container.
func (c *Container) Get(name string) (any, bool) {
	c.mu.RLock()
	v, ok := c.named[name]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if lp, ok := v.(*lazyProvider); ok {
		return lp.get(), true
	}
	return v, true
}

// MustGet retrieves a named instance or panics if not found.
func (c *Container) MustGet(name string) any {
	v, ok := c.Get(name)
	if !ok {
		panic("di: dependency not found: " + name)
	}
	return v
}

// Has reports whether a named instance exists.
func (c *Container) Has(name string) bool {
	c.mu.RLock()
	_, ok := c.named[name]
	c.mu.RUnlock()
	return ok
}

// Inject populates struct fields tagged with "inject".
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

		if dep, ok := c.named[name]; ok {
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

// Scope creates a child container with fresh scoped instances.
func (c *Container) Scope() *Container {
	c.mu.RLock()
	defer c.mu.RUnlock()

	child := New()
	child.parent = c
	for k, v := range c.services {
		if v.lifetime == Scoped {
			child.services[k] = &serviceEntry{
				lifetime: v.lifetime,
				factory:  v.factory,
			}
		} else {
			child.services[k] = v
		}
	}
	for k, v := range c.named {
		child.named[k] = v
	}
	return child
}

// ProvideLazy adds a lazily-initialized named instance.
func (c *Container) ProvideLazy(name string, factory func() any) {
	c.mu.Lock()
	c.named[name] = &lazyProvider{factory: factory}
	c.mu.Unlock()
}

// Clone returns a shallow copy of the container.
func (c *Container) Clone() *Container {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := New()
	for k, v := range c.services {
		clone.services[k] = v
	}
	for k, v := range c.named {
		clone.named[k] = v
	}
	return clone
}

// Keys returns all named instance keys.
func (c *Container) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.named))
	for k := range c.named {
		keys = append(keys, k)
	}
	return keys
}

// ResolveAllTyped returns all named instances implementing iface.
func (c *Container) ResolveAllTyped(iface reflect.Type) []any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []any
	for _, v := range c.named {
		if lp, ok := v.(*lazyProvider); ok {
			v = lp.get()
		}
		if reflect.TypeOf(v).Implements(iface) {
			result = append(result, v)
		}
	}
	return result
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