package di

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
	"github.com/mirkobrombin/go-foundation/v2/core/tags"
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
	buildMu      sync.Mutex
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
	services map[reflect.Type]*serviceEntry
	named    map[string]any
	mu       sync.RWMutex
	parent   *Container
	closers  []io.Closer
	closeMu  sync.Mutex
	closed   bool
}

var ErrContainerClosed = errors.New("di: container is closed")

var injectParser = tags.NewParser("inject", tags.WithPairDelimiter(";"), tags.WithKVSeparator(":"))

// New creates a new empty Container.
func New() *Container {
	return &Container{
		services: make(map[reflect.Type]*serviceEntry),
		named:    make(map[string]any),
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
	if factory == nil {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: factory for %v cannot be nil", typ))
		b.mu.Unlock()
		return
	}
	if _, exists := b.services[typ]; exists {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: type %v is already registered", typ))
		b.mu.Unlock()
		return
	}
	b.services[typ] = &serviceEntry{lifetime: lt, factory: factory, concreteType: typ}
	b.mu.Unlock()
}

// RegisterAs registers a service factory resolving to a different concrete type.
func RegisterAs[I, T any](b *Builder, factory func() T, lifetime ...Lifetime) {
	lt := Singleton
	if len(lifetime) > 0 {
		lt = lifetime[0]
	}
	typ := reflect.TypeOf((*I)(nil)).Elem()
	concreteType := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	if factory == nil {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: factory for %v cannot be nil", typ))
		b.mu.Unlock()
		return
	}
	if !concreteType.Implements(typ) {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: type %v does not implement %v", concreteType, typ))
		b.mu.Unlock()
		return
	}
	if _, exists := b.services[typ]; exists {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: type %v is already registered", typ))
		b.mu.Unlock()
		return
	}
	b.services[typ] = &serviceEntry{lifetime: lt, factory: factory, concreteType: concreteType}
	b.mu.Unlock()
}

// RegisterInstance registers a pre-created instance as a singleton.
func RegisterInstance[T any](b *Builder, instance T) {
	typ := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	if isNilInstance(instance) {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: instance for %v cannot be nil", typ))
		b.mu.Unlock()
		return
	}
	if _, exists := b.services[typ]; exists {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: type %v is already registered", typ))
		b.mu.Unlock()
		return
	}
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
	if !tTyp.Implements(iTyp) {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: type %v does not implement %v", tTyp, iTyp))
		b.mu.Unlock()
		return
	}
	if _, exists := b.services[iTyp]; exists {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: type %v is already registered", iTyp))
		b.mu.Unlock()
		return
	}
	b.services[iTyp] = &serviceEntry{lifetime: lt, factory: nil, concreteType: tTyp}
	b.mu.Unlock()
}

// RegisterFromFunc registers a service by constructor function with dependency injection.
func RegisterFromFunc[T any](b *Builder, constructor any, lifetime ...Lifetime) {
	lt := Singleton
	if len(lifetime) > 0 {
		lt = lifetime[0]
	}
	typ := reflect.TypeOf((*T)(nil)).Elem()

	ctorType := reflect.TypeOf(constructor)
	if ctorType == nil || ctorType.Kind() != reflect.Func {
		panic("di: RegisterFromFunc requires a function")
	}
	if ctorType.NumOut() < 1 || ctorType.NumOut() > 2 {
		panic("di: RegisterFromFunc constructor must return a value and optional error")
	}
	if !ctorType.Out(0).AssignableTo(typ) {
		panic(fmt.Sprintf("di: RegisterFromFunc constructor returns %v, want %v", ctorType.Out(0), typ))
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if ctorType.NumOut() == 2 && ctorType.Out(1) != errorType {
		panic("di: RegisterFromFunc second result must be error")
	}

	paramTypes := make([]reflect.Type, ctorType.NumIn())
	for i := 0; i < ctorType.NumIn(); i++ {
		paramTypes[i] = ctorType.In(i)
	}

	b.mu.Lock()
	if _, exists := b.services[typ]; exists {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: type %v is already registered", typ))
		b.mu.Unlock()
		return
	}
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
	buildErrors := append([]error(nil), b.buildErrors...)
	for k, v := range b.services {
		c.services[k] = cloneServiceEntry(v)
	}
	for k, v := range b.named {
		c.named[k] = v
	}
	b.mu.RUnlock()

	if len(buildErrors) > 0 {
		return c, fmt.Errorf("di: build validation failed: %v", buildErrors)
	}
	if errs := c.validate(); len(errs) > 0 {
		return c, fmt.Errorf("di: build validation failed: %v", errs)
	}
	for _, entry := range c.services {
		if entry.built {
			_ = c.trackCloser(entry.instance)
		}
	}
	for _, instance := range c.named {
		if _, lazy := instance.(*lazyProvider); !lazy {
			_ = c.trackCloser(instance)
		}
	}
	return c, nil
}

func cloneServiceEntry(entry *serviceEntry) *serviceEntry {
	return &serviceEntry{
		lifetime:     entry.lifetime,
		factory:      entry.factory,
		constructor:  entry.constructor,
		instance:     entry.instance,
		built:        entry.built,
		concreteType: entry.concreteType,
		paramTypes:   append([]reflect.Type(nil), entry.paramTypes...),
	}
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
	states := make(map[reflect.Type]uint8)

	for typ := range c.services {
		if err := c.validateType(typ, states); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (c *Container) validateType(typ reflect.Type, states map[reflect.Type]uint8) error {
	switch states[typ] {
	case 1:
		return fmt.Errorf("di: circular dependency detected for type %v", typ)
	case 2:
		return nil
	}
	states[typ] = 1
	defer func() {
		if states[typ] == 1 {
			delete(states, typ)
		}
	}()

	entry, ok := c.services[typ]
	if !ok {
		return &ResolveError{Type: typ}
	}

	if entry.constructor != nil {
		for _, pt := range entry.paramTypes {
			if err := c.validateType(pt, states); err != nil {
				return fmt.Errorf("di: type %v requires %v: %w", typ, pt, err)
			}
		}
	}

	if entry.factory == nil && entry.constructor == nil {
		if err := c.validateConcreteType(entry.concreteType, states); err != nil {
			return err
		}
	}

	states[typ] = 2
	return nil
}

func (c *Container) validateConcreteType(typ reflect.Type, states map[reflect.Type]uint8) error {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("di: auto-construction requires struct type, got %v", typ)
	}

	if initMethod, ok := reflect.PointerTo(typ).MethodByName("Init"); ok {
		errorType := reflect.TypeOf((*error)(nil)).Elem()
		if initMethod.Type.NumOut() > 1 ||
			initMethod.Type.NumOut() == 1 && initMethod.Type.Out(0) != errorType {
			return fmt.Errorf("di: type %v Init must return nothing or error", typ)
		}
		for i := 1; i < initMethod.Type.NumIn(); i++ {
			paramType := initMethod.Type.In(i)
			if err := c.validateType(paramType, states); err != nil {
				return fmt.Errorf("di: type %v Init requires %v: %w", typ, paramType, err)
			}
		}
		return nil
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("inject")
		if tag == "" {
			continue
		}
		if !field.IsExported() {
			return fmt.Errorf("di: struct %v injected field %s must be exported", typ, field.Name)
		}
		if err := c.validateType(field.Type, states); err != nil {
			return fmt.Errorf(
				"di: struct %v field %s requires %v: %w",
				typ,
				field.Name,
				field.Type,
				err,
			)
		}
	}
	return nil
}

func (b *Builder) Provide(name string, instance any) {
	if isNilInstance(instance) {
		b.mu.Lock()
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: dependency %q cannot be nil", name))
		b.mu.Unlock()
		return
	}
	contracts.MustVerify(instance)
	b.mu.Lock()
	if name == "" {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: dependency name cannot be empty"))
		b.mu.Unlock()
		return
	}
	if _, exists := b.named[name]; exists {
		b.buildErrors = append(b.buildErrors, fmt.Errorf("di: dependency %q is already registered", name))
		b.mu.Unlock()
		return
	}
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
	return resolveByType(c, typ, make(map[reflect.Type]struct{}))
}

func resolveByType(c *Container, typ reflect.Type, stack map[reflect.Type]struct{}) (reflect.Value, error) {
	if err := c.ensureOpen(); err != nil {
		return reflect.Value{}, err
	}
	c.mu.RLock()
	entry, ok := c.services[typ]
	c.mu.RUnlock()

	if !ok {
		if c.parent != nil {
			return resolveByType(c.parent, typ, stack)
		}
		return reflect.Value{}, &ResolveError{Type: typ}
	}
	if entry.lifetime == Singleton && c.parent != nil {
		return resolveByType(c.parent, typ, stack)
	}
	if _, resolving := stack[typ]; resolving {
		return reflect.Value{}, fmt.Errorf("di: circular dependency detected for type %v", typ)
	}
	stack[typ] = struct{}{}
	defer func() {
		delete(stack, typ)
	}()

	switch entry.lifetime {
	case Singleton, Scoped:
		entry.buildMu.Lock()
		defer entry.buildMu.Unlock()
		if entry.built {
			return reflect.ValueOf(entry.instance), nil
		}
		result, err := invokeEntry(c, entry, stack)
		if err != nil {
			return reflect.Value{}, err
		}
		if isNilReflectValue(result) {
			return reflect.Value{}, fmt.Errorf("di: factory for %v returned nil", typ)
		}
		entry.instance = result.Interface()
		entry.built = true
		if err := c.trackCloser(entry.instance); err != nil {
			return reflect.Value{}, err
		}
		return result, nil
	case Transient:
		result, err := invokeEntry(c, entry, stack)
		if err != nil {
			return reflect.Value{}, err
		}
		if isNilReflectValue(result) {
			return reflect.Value{}, fmt.Errorf("di: factory for %v returned nil", typ)
		}
		if err := c.trackCloser(result.Interface()); err != nil {
			return reflect.Value{}, err
		}
		return result, nil
	default:
		return reflect.Value{}, &ResolveError{Type: typ}
	}
}

func (c *Container) trackCloser(instance any) error {
	if closer, ok := instance.(io.Closer); ok {
		c.closeMu.Lock()
		if c.closed {
			c.closeMu.Unlock()
			_ = closer.Close()
			return ErrContainerClosed
		}
		c.trackCloserLocked(closer)
		c.closeMu.Unlock()
	}
	return nil
}

func (c *Container) trackCloserLocked(closer io.Closer) {
	for _, existing := range c.closers {
		if sameCloser(existing, closer) {
			return
		}
	}
	c.closers = append(c.closers, closer)
}

func (c *Container) ensureOpen() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return ErrContainerClosed
	}
	return nil
}

func sameCloser(left, right io.Closer) bool {
	leftType := reflect.TypeOf(left)
	rightType := reflect.TypeOf(right)
	if leftType != rightType || leftType == nil || !leftType.Comparable() {
		return false
	}
	return left == right
}

// Close calls Close() on all resolved services that implement io.Closer.
// Call this at the end of a scope (e.g. at end of HTTP request).
func (c *Container) Close() error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return nil
	}
	c.closed = true
	closers := make([]io.Closer, len(c.closers))
	copy(closers, c.closers)
	c.closers = nil
	c.closeMu.Unlock()

	var errs []error
	for index := len(closers) - 1; index >= 0; index-- {
		if err := closers[index].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("di: close errors: %v", errs)
	}
	return nil
}

func invokeEntry(c *Container, entry *serviceEntry, stack map[reflect.Type]struct{}) (reflect.Value, error) {
	if entry.constructor != nil {
		return invokeConstructor(c, entry, stack)
	}
	if entry.factory != nil {
		results := reflect.ValueOf(entry.factory).Call(nil)
		return results[0], nil
	}
	return constructValue(c, entry.concreteType, stack)
}

func invokeConstructor(c *Container, entry *serviceEntry, stack map[reflect.Type]struct{}) (reflect.Value, error) {
	ctorVal := reflect.ValueOf(entry.constructor)
	args := make([]reflect.Value, len(entry.paramTypes))
	for i, pt := range entry.paramTypes {
		resolved, err := resolveByType(c, pt, stack)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("di: cannot resolve param %d (%v) of constructor for %v: %w", i, pt, entry.concreteType, err)
		}
		args[i] = resolved
	}
	results := ctorVal.Call(args)
	if len(results) == 2 && !results[1].IsNil() {
		return reflect.Value{}, results[1].Interface().(error)
	}
	return results[0], nil
}

func constructValue(c *Container, typ reflect.Type, stack map[reflect.Type]struct{}) (reflect.Value, error) {
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
			resolved, err := resolveByType(c, ctorType.In(i), stack)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("di: cannot resolve param %d of Init: %w", i, err)
			}
			args[i] = resolved
		}
		results := ctorVal.Call(args)
		if len(results) == 1 && !results[0].IsNil() {
			return reflect.Value{}, results[0].Interface().(error)
		}
	} else {
		elem := val.Elem()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := field.Tag.Get("inject")
			if tag == "" {
				continue
			}
			fieldVal := elem.Field(i)
			if !fieldVal.CanSet() || !field.IsExported() {
				return reflect.Value{}, fmt.Errorf("di: injected field %s must be exported and settable", field.Name)
			}
			resolved, err := resolveByType(c, fieldVal.Type(), stack)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("di: cannot resolve field %s: %w", field.Name, err)
			}
			fieldVal.Set(resolved)
		}
	}

	return val, nil
}

func (c *Container) Provide(name string, instance any) {
	if isNilInstance(instance) {
		panic(fmt.Sprintf("di: dependency %q cannot be nil", name))
	}
	contracts.MustVerify(instance)
	if name == "" {
		panic("di: dependency name cannot be empty")
	}
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		panic(ErrContainerClosed)
	}
	c.mu.Lock()
	if _, exists := c.named[name]; exists {
		c.mu.Unlock()
		c.closeMu.Unlock()
		panic(fmt.Sprintf("di: dependency %q is already registered", name))
	}
	c.named[name] = instance
	c.mu.Unlock()
	if closer, ok := instance.(io.Closer); ok {
		c.trackCloserLocked(closer)
	}
	c.closeMu.Unlock()
}

func (c *Container) Get(name string) (any, bool) {
	if c.ensureOpen() != nil {
		return nil, false
	}
	c.mu.RLock()
	v, ok := c.named[name]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if lp, ok := v.(*lazyProvider); ok {
		value, err := lp.get()
		if err != nil {
			return nil, false
		}
		if lp.owner.trackCloser(value) != nil {
			return nil, false
		}
		return value, true
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
	if c.ensureOpen() != nil {
		return false
	}
	c.mu.RLock()
	_, ok := c.named[name]
	c.mu.RUnlock()
	return ok
}

// ValidateTarget checks named injection fields without changing target.
func (c *Container) ValidateTarget(target any) error {
	if err := c.ensureOpen(); err != nil {
		return err
	}
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("di: injection target must be a pointer to a struct")
	}
	fields := injectParser.ParseStruct(target)

	for _, meta := range fields {
		field := val.Elem().Field(meta.Index)
		if !field.CanSet() {
			return fmt.Errorf("di: injected field %s must be exported and settable", meta.Name)
		}
		name := meta.RawTag
		if name == "" {
			name = meta.Name
		}
		c.mu.RLock()
		dep, ok := c.named[name]
		c.mu.RUnlock()
		if !ok {
			return fmt.Errorf("di: dependency %q required by field %s is not registered", name, meta.Name)
		}
		depType := reflect.TypeOf(dep)
		if lazy, isLazy := dep.(*lazyProvider); isLazy {
			depType = lazy.resultType
			if depType == nil {
				return fmt.Errorf(
					"di: lazy dependency %q has no type metadata; use a typed key or provide its result type",
					name,
				)
			}
		}
		if depType == nil || !depType.AssignableTo(field.Type()) {
			return fmt.Errorf("di: dependency %q has type %v, field %s requires %v", name, depType, meta.Name, field.Type())
		}
	}
	return nil
}

// Inject validates and populates named injection fields.
func (c *Container) Inject(target any) error {
	if err := c.ValidateTarget(target); err != nil {
		return err
	}

	elem := reflect.ValueOf(target).Elem()
	fields := injectParser.ParseStruct(target)

	for _, meta := range fields {
		field := elem.Field(meta.Index)
		if !field.CanSet() {
			return fmt.Errorf("di: injected field %s must be exported and settable", meta.Name)
		}
		name := meta.RawTag
		if name == "" {
			name = meta.Name
		}
		dep, _ := c.Get(name)
		field.Set(reflect.ValueOf(dep))
	}
	return nil
}

// Scope creates a child container with fresh scoped instances.
// When the scope ends, call Close() to dispose scoped services.
func (c *Container) Scope() *Container {
	if err := c.ensureOpen(); err != nil {
		panic(err)
	}
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

func (c *Container) ProvideLazy(name string, factory func() any, resultType ...reflect.Type) {
	if name == "" {
		panic("di: dependency name cannot be empty")
	}
	if factory == nil {
		panic("di: lazy dependency factory cannot be nil")
	}
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		panic(ErrContainerClosed)
	}
	c.mu.Lock()
	if _, exists := c.named[name]; exists {
		c.mu.Unlock()
		c.closeMu.Unlock()
		panic(fmt.Sprintf("di: dependency %q is already registered", name))
	}
	var typ reflect.Type
	if len(resultType) > 0 {
		typ = resultType[0]
	}
	c.named[name] = &lazyProvider{factory: factory, owner: c, resultType: typ}
	c.mu.Unlock()
	c.closeMu.Unlock()
}

func (c *Container) Clone() *Container {
	if err := c.ensureOpen(); err != nil {
		panic(err)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := New()
	clone.parent = c
	for k, v := range c.services {
		clone.services[k] = v
	}
	for k, v := range c.named {
		clone.named[k] = v
	}
	return clone
}

func (c *Container) Keys() []string {
	if c.ensureOpen() != nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.named))
	for k := range c.named {
		keys = append(keys, k)
	}
	return keys
}

func (c *Container) ResolveAllTyped(iface reflect.Type) []any {
	if c.ensureOpen() != nil {
		return nil
	}
	c.mu.RLock()
	values := make([]any, 0, len(c.named))
	for _, value := range c.named {
		values = append(values, value)
	}
	c.mu.RUnlock()

	var result []any
	for _, v := range values {
		if lp, ok := v.(*lazyProvider); ok {
			var err error
			v, err = lp.get()
			if err != nil {
				continue
			}
			if lp.owner.trackCloser(v) != nil {
				continue
			}
		}
		if !isNilInstance(v) && reflect.TypeOf(v).Implements(iface) {
			result = append(result, v)
		}
	}
	return result
}

func isNilInstance(instance any) bool {
	if instance == nil {
		return true
	}
	value := reflect.ValueOf(instance)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isNilReflectValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type lazyProvider struct {
	once       sync.Once
	value      any
	err        error
	factory    func() any
	owner      *Container
	resultType reflect.Type
}

func (l *lazyProvider) get() (any, error) {
	l.once.Do(func() {
		l.value = l.factory()
		if isNilInstance(l.value) {
			l.err = errors.New("di: lazy dependency factory returned nil")
		}
	})
	return l.value, l.err
}
