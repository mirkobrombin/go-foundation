package actions

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/events"
	"github.com/mirkobrombin/go-foundation/pkg/options"
	"github.com/mirkobrombin/go-foundation/pkg/reflectutil"
)

// Handler is the interface for declarative actions.
type Handler interface {
	Handle(ctx context.Context) (any, error)
}

type actionMeta struct {
	name      string
	key       string
	prototype reflect.Type
	value     reflect.Value
}

// Router dispatches named actions and optional key bindings.
type Router struct {
	mu          sync.RWMutex
	handlers    map[string]actionMeta
	keys        map[string]string
	container   *di.Container
	builder     *di.Builder
	bus         *events.Bus
	asyncEvents bool
}

// Option configures a Router.
type Option = options.Option[Router]

// New creates a Router.
func New(opts ...Option) *Router {
	r := &Router{
		handlers: make(map[string]actionMeta),
		keys:     make(map[string]string),
		builder:  di.NewBuilder(),
	}
	options.Apply(r, opts...)
	return r
}

// WithContainer uses an existing DI container.
func WithContainer(container *di.Container) Option {
	return func(r *Router) {
		r.container = container
	}
}

// WithEvents emits the action instance after dispatch.
func WithEvents(bus *events.Bus) Option {
	return func(r *Router) {
		r.bus = bus
		r.asyncEvents = false
	}
}

// WithAsyncEvents emits the action instance asynchronously after dispatch.
func WithAsyncEvents(bus *events.Bus) Option {
	return func(r *Router) {
		r.bus = bus
		r.asyncEvents = true
	}
}

// UseContainer replaces the DI container used during dispatch.
func (r *Router) UseContainer(container *di.Container) {
	r.mu.Lock()
	r.container = container
	r.mu.Unlock()
}

// UseEvents emits the action instance after dispatch.
func (r *Router) UseEvents(bus *events.Bus) {
	r.mu.Lock()
	r.bus = bus
	r.asyncEvents = false
	r.mu.Unlock()
}

// UseAsyncEvents emits the action instance asynchronously after dispatch.
func (r *Router) UseAsyncEvents(bus *events.Bus) {
	r.mu.Lock()
	r.bus = bus
	r.asyncEvents = true
	r.mu.Unlock()
}

// Provide registers a named dependency for action handlers.
func (r *Router) Provide(name string, instance any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.builder == nil {
		r.builder = di.NewBuilder()
	}
	r.builder.Provide(name, instance)
	if r.container != nil {
		r.container.Provide(name, instance)
	}
}

// Build constructs the DI container used during dispatch.
func (r *Router) Build() (*di.Container, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.container != nil {
		return r.container, nil
	}
	if r.builder == nil {
		r.builder = di.NewBuilder()
	}
	container, err := r.builder.Build()
	if err != nil {
		return nil, err
	}
	r.container = container
	return container, nil
}

// Register registers an action handler.
func (r *Router) Register(prototype Handler) {
	meta := parseAction(prototype)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.handlers[meta.name]; ok {
		panic(fmt.Sprintf("actions: handler %q already registered", meta.name))
	}
	r.handlers[meta.name] = meta
	if meta.key != "" {
		if existing, ok := r.keys[meta.key]; ok {
			panic(fmt.Sprintf("actions: key %q already bound to %q", meta.key, existing))
		}
		r.keys[meta.key] = meta.name
	}
}

// Dispatch executes an action by name.
func (r *Router) Dispatch(ctx context.Context, name string, payload ...any) (any, error) {
	r.mu.RLock()
	meta, ok := r.handlers[name]
	container := r.container
	bus := r.bus
	asyncEvents := r.asyncEvents
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("actions: no handler for %q", name)
	}

	if container == nil {
		var err error
		container, err = r.Build()
		if err != nil {
			return nil, err
		}
	}

	instance := newAction(meta)
	container.Inject(instance)
	if len(payload) > 0 && payload[0] != nil {
		if err := bindPayload(instance, payload[0]); err != nil {
			return nil, err
		}
	}

	result, err := instance.(Handler).Handle(ctx)
	eventErr := emitAction(ctx, bus, asyncEvents, instance)
	if err != nil {
		return result, errors.Join(err, eventErr)
	}
	return result, eventErr
}

// DispatchKey executes an action by key binding.
func (r *Router) DispatchKey(ctx context.Context, key string, payload ...any) (any, error) {
	r.mu.RLock()
	name, ok := r.keys[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("actions: no action bound to %q", key)
	}
	return r.Dispatch(ctx, name, payload...)
}

// Has returns true if an action is registered.
func (r *Router) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[name]
	return ok
}

// Actions returns registered action names.
func (r *Router) Actions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// KeyBindings returns key-to-action mappings.
func (r *Router) KeyBindings() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	bindings := make(map[string]string, len(r.keys))
	for key, name := range r.keys {
		bindings[key] = name
	}
	return bindings
}

func parseAction(prototype Handler) actionMeta {
	val := reflect.ValueOf(prototype)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		panic("actions: prototype must be a pointer to a struct")
	}

	typ := val.Elem().Type()
	var name, key string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if action := field.Tag.Get("action"); action != "" {
			name = action
		}
		if keys := field.Tag.Get("keys"); keys != "" {
			key = keys
		}
	}
	if name == "" {
		panic("actions: struct must have an action tag")
	}

	return actionMeta{
		name:      name,
		key:       key,
		prototype: typ,
		value:     val.Elem(),
	}
}

func newAction(meta actionMeta) any {
	newVal := reflect.New(meta.prototype).Elem()
	newVal.Set(meta.value)
	return newVal.Addr().Interface()
}

func bindPayload(target any, payload any) error {
	src := reflect.ValueOf(payload)
	if src.Kind() == reflect.Ptr {
		if src.IsNil() {
			return nil
		}
		src = src.Elem()
	}

	switch src.Kind() {
	case reflect.Map:
		for _, key := range src.MapKeys() {
			if err := setPayloadField(target, fmt.Sprint(key.Interface()), src.MapIndex(key)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		srcType := src.Type()
		for i := 0; i < src.NumField(); i++ {
			field := srcType.Field(i)
			name := field.Name
			if tag := field.Tag.Get("json"); tag != "" {
				name = strings.Split(tag, ",")[0]
			}
			if name == "-" || name == "" {
				continue
			}
			if err := setPayloadField(target, name, src.Field(i)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("actions: payload must be a map or struct")
	}
	return nil
}

func setPayloadField(target any, name string, value reflect.Value) error {
	dst := reflect.ValueOf(target).Elem()
	dstType := dst.Type()

	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface && value.IsNil() {
		return nil
	}
	if value.Kind() == reflect.Interface && !value.IsNil() {
		value = value.Elem()
	}

	for i := 0; i < dst.NumField(); i++ {
		fieldMeta := dstType.Field(i)
		field := dst.Field(i)
		if !field.CanSet() {
			continue
		}
		if !fieldNameMatches(fieldMeta, name) {
			continue
		}
		if value.IsValid() && value.Type().AssignableTo(field.Type()) {
			field.Set(value)
			return nil
		}
		if value.IsValid() && value.Type().ConvertibleTo(field.Type()) {
			field.Set(value.Convert(field.Type()))
			return nil
		}
		if value.IsValid() && value.Kind() == reflect.String {
			return reflectutil.Bind(field, value.String())
		}
		return fmt.Errorf("actions: cannot bind %s", fieldMeta.Name)
	}
	return nil
}

func fieldNameMatches(field reflect.StructField, name string) bool {
	if strings.EqualFold(field.Name, name) {
		return true
	}
	if tag := field.Tag.Get("json"); tag != "" {
		if strings.Split(tag, ",")[0] == name {
			return true
		}
	}
	if tag := field.Tag.Get("action"); tag != "" {
		return tag == name
	}
	return false
}

func emitAction(ctx context.Context, bus *events.Bus, async bool, event any) error {
	if bus == nil {
		return nil
	}
	if async {
		events.EmitAnyAsync(ctx, bus, event)
		return nil
	}
	return events.EmitAny(ctx, bus, event)
}
