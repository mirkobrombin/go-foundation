package di

import (
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/contracts"
	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// Lifetime controls how services are instantiated.
type Lifetime int

const (
	// Transient creates a new instance every time.
	Transient Lifetime = iota
	// Singleton creates one instance shared across the container.
	Singleton
	// Scoped creates one instance per scoped container.
	Scoped
)

type serviceEntry struct {
	lifetime     Lifetime
	factory      any
	constructor  any
	instance     any
	built        bool
	concreteType reflect.Type
	paramTypes   []reflect.Type
}

// ResolveError is returned when a type cannot be resolved.
type ResolveError struct {
	Type reflect.Type
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("di: cannot resolve type %v", e.Type)
}

// Container is the dependency injection container.
type Container struct {
	services  map[reflect.Type]*serviceEntry
	named     map[string]any
	mu        sync.RWMutex
	parent    *Container
	resolving map[reflect.Type]struct{}
	muResolve sync.Mutex
	closers   []io.Closer
	closeMu   sync.Mutex
}

var injectParser = tags.NewParser("inject", tags.WithPairDelimiter(";"), tags.WithKVSeparator(":"), tags.WithIncludeUntagged())

// New creates a new empty Container.
func New() *Container {
	return &Container{
		services:  make(map[reflect.Type]*serviceEntry),
		named:     make(map[string]any),
		resolving: make(map[reflect.Type]struct{}),
	}
}

// Builder constructs a Container with validation.
type Builder struct {
	services    map[reflect.Type]*serviceEntry
	named       map[string]any
	mu          sync.RWMutex
	validated   bool
	buildErrors []error
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{
		services: make(map[reflect.Type]*serviceEntry),
		named:    make(map[string]any),
	}
}

// Register adds a service factory to the builder.
func Register[T any](b *Builder, factory func() T, lifetime ...Lifetime) {
	lt := Singleton
	if len(lifetime) > 0 {
		lt = lifetime[0]
	}
	typ := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	b.services[typ] = &serviceEntry{lifetime: lt, factory: factory, concreteType: typ}
	b.mu.Unlock()
}

// RegisterAs registers a service factory resolving to a different concrete type.
func RegisterAs[T any](b *Builder, factory func() T, lifetime ...Lifetime) {
	lt := Singleton
	if len(lifetime) > 0 {
		lt = lifetime[0]
	}
	typ := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	b.services[typ] = &serviceEntry{lifetime: lt, factory: factory, concreteType: typ}
	b.mu.Unlock()
}

// RegisterInstance registers a pre-created instance as a singleton.
func RegisterInstance[T any](b *Builder, instance T) {
	typ := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	b.services[typ] = &serviceEntry{
		lifetime:     Singleton,
		concreteType: typ,
		factory:      func() T { return instance },
		built:        true,
		instance:     instance,
	}
	b.mu.Unlock()
}

// RegisterImpl registers a concrete type T that satisfies interface I.
func RegisterImpl[I, T any](b *Builder, lifetime ...Lifetime) {
	lt := Singleton
	if len(lifetime) > 0 {
		lt = lifetime[0]
	}
	iTyp := reflect.TypeOf((*I)(nil)).Elem()
	tTyp := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	b.services[iTyp] = &serviceEntry{lifetime: lt, factory: nil, concreteType: tTyp}
	b.mu.Unlock()
}

// RegisterFromFunc registers a type using a constructor function whose
// parameters are auto-resolved from the container at resolution time.
// T is the return type. The constructor must accept resolvable types.
// Build() validates that all parameter types are registered.
// RegisterFromFunc registers a service by constructor function with dependency injection.
func RegisterFromFunc[T any](b *Builder, constructor any, lifetime ...Lifetime) {
	lt := Singleton
	if len(lifetime) > 0 {
		lt = lifetime[0]
	}
	typ := reflect.TypeOf((*T)(nil)).Elem()

	ctorType := reflect.TypeOf(constructor)
	if ctorType.Kind() != reflect.Func {
		panic("di: RegisterFromFunc requires a function")
	}
	if ctorType.NumOut() == 0 {
		panic("di: RegisterFromFunc: constructor must return a value")
	}

	paramTypes := make([]reflect.Type, ctorType.NumIn())
	for i := 0; i < ctorType.NumIn(); i++ {
		paramTypes[i] = ctorType.In(i)
	}

	b.mu.Lock()
	b.services[typ] = &serviceEntry{
		lifetime: lt, factory: nil, concreteType: typ,
		constructor: constructor, paramTypes: paramTypes,
	}
	b.mu.Unlock()
}

// Build creates a Container and validates all registrations.
// Returns an error if any registered type has unresolvable dependencies.
func (b *Builder) Build() (*Container, error) {
	c := New()
	b.mu.RLock()
	for k, v := range b.services {
		cp := *v
		c.services[k] = &cp
	}
	for k, v := range b.named {
		c.named[k] = v
	}
	b.mu.RUnlock()

	if errs := c.validate(); len(errs) > 0 {
		return c, fmt.Errorf("di: build validation failed: %v", errs)
	}
	return c, nil
}

// MustBuild is like Build but panics on validation errors.
func (b *Builder) MustBuild() *Container {
	c, err := b.Build()
	if err != nil {
		panic(err.Error())
	}
	return c
}

func (c *Container) validate() []error {
	var errs []error
	visited := make(map[reflect.Type]bool)

	for typ := range c.services {
		if err := c.validateType(typ, visited); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (c *Container) validateType(typ reflect.Type, visited map[reflect.Type]bool) error {
	if visited[typ] {
		return nil
	}
	visited[typ] = true

	entry, ok := c.services[typ]
	if !ok {
		return &ResolveError{Type: typ}
	}

	if entry.constructor != nil && entry.paramTypes != nil {
		for _, pt := range entry.paramTypes {
			if _, ok := c.services[pt]; !ok {
				return fmt.Errorf("di: type %v requires %v which is not registered", typ, pt)
			}
		}
	}

	if entry.factory == nil && entry.concreteType.Kind() == reflect.Struct {
		if err := c.validateStructFields(entry.concreteType, visited); err != nil {
			return err
		}
	}

	return nil
}

func (c *Container) validateStructFields(typ reflect.Type, visited map[reflect.Type]bool) error {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("inject")
		if tag == "" {
			continue
		}
		if _, ok := c.services[field.Type]; !ok {
			return fmt.Errorf("di: struct %v field %s requires %v which is not registered", typ, field.Name, field.Type)
		}
	}
	return nil
}

func (b *Builder) Provide(name string, instance any) {
	contracts.MustVerify(instance)
	b.mu.Lock()
	b.named[name] = instance
	b.mu.Unlock()
}

// ResolveType resolves a service from the container, panicking on failure.
func ResolveType[T any](c *Container) T {
	result, err := tryResolveType(c, reflect.TypeOf((*T)(nil)).Elem())
	if err != nil {
		panic(err.Error())
	}
	return result.Interface().(T)
}

// TryResolveType resolves a service from the container, returning an error on failure.
func TryResolveType[T any](c *Container) (T, error) {
	var zero T
	result, err := tryResolveType(c, reflect.TypeOf((*T)(nil)).Elem())
	if err != nil {
		return zero, err
	}
	return result.Interface().(T), nil
}

func tryResolveType(c *Container, typ reflect.Type) (reflect.Value, error) {
	c.mu.RLock()
	entry, ok := c.services[typ]
	c.mu.RUnlock()

	if !ok {
		if c.parent != nil {
			return tryResolveType(c.parent, typ)
		}
		return reflect.Value{}, &ResolveError{Type: typ}
	}

	c.muResolve.Lock()
	if _, resolving := c.resolving[typ]; resolving {
		c.muResolve.Unlock()
		return reflect.Value{}, fmt.Errorf("di: circular dependency detected for type %v", typ)
	}
	c.resolving[typ] = struct{}{}
	c.muResolve.Unlock()

	defer func() {
		c.muResolve.Lock()
		delete(c.resolving, typ)
		c.muResolve.Unlock()
	}()

	switch entry.lifetime {
	case Singleton:
		if entry.built {
			return reflect.ValueOf(entry.instance), nil
		}
		c.mu.Lock()
		if entry.built {
			c.mu.Unlock()
			return reflect.ValueOf(entry.instance), nil
		}
		c.mu.Unlock()
		result, err := invokeEntry(c, entry)
		if err != nil {
			return reflect.Value{}, err
		}
		c.mu.Lock()
		entry.instance = result.Interface()
		entry.built = true
		c.mu.Unlock()
		c.trackCloser(entry.instance)
		return result, nil
	case Transient:
		result, err := invokeEntry(c, entry)
		if err != nil {
			return reflect.Value{}, err
		}
		c.trackCloser(result.Interface())
		return result, nil
	case Scoped:
		if entry.built {
			return reflect.ValueOf(entry.instance), nil
		}
		c.mu.Lock()
		if entry.built {
			c.mu.Unlock()
			return reflect.ValueOf(entry.instance), nil
		}
		c.mu.Unlock()
		result, err := invokeEntry(c, entry)
		if err != nil {
			return reflect.Value{}, err
		}
		c.mu.Lock()
		entry.instance = result.Interface()
		entry.built = true
		c.mu.Unlock()
		c.trackCloser(entry.instance)
		return result, nil
	default:
		return reflect.Value{}, &ResolveError{Type: typ}
	}
}

func (c *Container) trackCloser(instance any) {
	if closer, ok := instance.(io.Closer); ok {
		c.closeMu.Lock()
		c.closers = append(c.closers, closer)
		c.closeMu.Unlock()
	}
}

// Close calls Close() on all resolved services that implement io.Closer.
// Call this at the end of a scope (e.g. at end of HTTP request).
func (c *Container) Close() error {
	c.closeMu.Lock()
	closers := make([]io.Closer, len(c.closers))
	copy(closers, c.closers)
	c.closers = nil
	c.closeMu.Unlock()

	var errs []error
	for _, closer := range closers {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("di: close errors: %v", errs)
	}
	return nil
}

func invokeEntry(c *Container, entry *serviceEntry) (reflect.Value, error) {
	if entry.constructor != nil && entry.paramTypes != nil {
		return invokeConstructor(c, entry)
	}
	if entry.factory != nil {
		results := reflect.ValueOf(entry.factory).Call(nil)
		return results[0], nil
	}
	return constructValue(c, entry.concreteType)
}

func invokeConstructor(c *Container, entry *serviceEntry) (reflect.Value, error) {
	ctorVal := reflect.ValueOf(entry.constructor)
	args := make([]reflect.Value, len(entry.paramTypes))
	for i, pt := range entry.paramTypes {
		resolved, err := resolveByType(c, pt)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("di: cannot resolve param %d (%v) of constructor for %v: %w", i, pt, entry.concreteType, err)
		}
		args[i] = resolved
	}
	results := ctorVal.Call(args)
	return results[0], nil
}

func constructValue(c *Container, typ reflect.Type) (reflect.Value, error) {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("di: auto-construction requires struct type, got %v", typ)
	}

	val := reflect.New(typ)

	ctorVal := val.MethodByName("Init")
	if ctorVal.IsValid() {
		ctorType := ctorVal.Type()
		args := make([]reflect.Value, ctorType.NumIn())
		for i := 0; i < ctorType.NumIn(); i++ {
			resolved, err := resolveByType(c, ctorType.In(i))
			if err != nil {
				return reflect.Value{}, fmt.Errorf("di: cannot resolve param %d of Init: %w", i, err)
			}
			args[i] = resolved
		}
		ctorVal.Call(args)
	} else {
		elem := val.Elem()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := field.Tag.Get("inject")
			if tag == "" && !field.Anonymous {
				continue
			}
			fieldVal := elem.Field(i)
			if !fieldVal.CanSet() || !field.IsExported() {
				continue
			}
			resolved, err := resolveByType(c, fieldVal.Type())
			if err != nil {
				return reflect.Value{}, fmt.Errorf("di: cannot resolve field %s: %w", field.Name, err)
			}
			fieldVal.Set(resolved)
		}
	}

	return val, nil
}

func resolveByType(c *Container, typ reflect.Type) (reflect.Value, error) {
	c.mu.RLock()
	entry, ok := c.services[typ]
	c.mu.RUnlock()
	if !ok && c.parent != nil {
		return resolveByType(c.parent, typ)
	}
	if !ok {
		return reflect.Value{}, &ResolveError{Type: typ}
	}
	return invokeEntry(c, entry)
}

func (c *Container) Provide(name string, instance any) {
	contracts.MustVerify(instance)
	c.mu.Lock()
	c.named[name] = instance
	c.mu.Unlock()
}

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

func (c *Container) MustGet(name string) any {
	v, ok := c.Get(name)
	if !ok {
		panic("di: dependency not found: " + name)
	}
	return v
}

func (c *Container) Has(name string) bool {
	c.mu.RLock()
	_, ok := c.named[name]
	c.mu.RUnlock()
	return ok
}

func (c *Container) Inject(target any) {
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return
	}
	elem := val.Elem()
	fields := injectParser.ParseStruct(target)

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
// When the scope ends, call Close() to dispose scoped services.
func (c *Container) Scope() *Container {
	c.mu.RLock()
	defer c.mu.RUnlock()

	child := New()
	child.parent = c
	for k, v := range c.services {
		if v.lifetime == Scoped {
			child.services[k] = &serviceEntry{
				lifetime:     v.lifetime,
				factory:      v.factory,
				concreteType: v.concreteType,
				constructor:  v.constructor,
				paramTypes:   v.paramTypes,
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

func (c *Container) ProvideLazy(name string, factory func() any) {
	c.mu.Lock()
	c.named[name] = &lazyProvider{factory: factory}
	c.mu.Unlock()
}

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

func (c *Container) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.named))
	for k := range c.named {
		keys = append(keys, k)
	}
	return keys
}

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