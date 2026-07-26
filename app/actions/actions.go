package actions

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/core/events"
	"github.com/mirkobrombin/go-foundation/v2/core/options"
	"github.com/mirkobrombin/go-foundation/v2/core/reflectutil"
)

// Handler is the interface for declarative actions.
type Handler interface {
	Handle(ctx context.Context) (any, error)
}

// Definition describes an action without runtime metadata discovery.
type Definition struct {
	Name string
	Key  string
	New  func() Handler
}

type actionMeta struct {
	name string
	key  string
	new  func() Handler
}

// Router dispatches named actions and optional key bindings.
type Router struct {
	mu          sync.RWMutex
	handlers    map[string]actionMeta
	typed       map[string]func(context.Context, any) (any, error)
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
		typed:    make(map[string]func(context.Context, any) (any, error)),
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
	if err := r.register(meta); err != nil {
		panic(err)
	}
}

// RegisterDefinition registers a statically described action.
func (r *Router) RegisterDefinition(def Definition) error {
	return r.RegisterDefinitions(def)
}

// RegisterDefinitions validates and registers static actions as one operation.
func (r *Router) RegisterDefinitions(defs ...Definition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	metas, err := r.validateDefinitionsLocked(r.container, defs)
	if err != nil {
		return err
	}
	for _, meta := range metas {
		r.handlers[meta.name] = meta
		if meta.key != "" {
			r.keys[meta.key] = meta.name
		}
	}
	return nil
}

// ValidateDefinitions checks static actions without changing the router.
func (r *Router) ValidateDefinitions(container *di.Container, defs ...Definition) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, err := r.validateDefinitionsLocked(container, defs)
	return err
}

func (r *Router) validateDefinitionsLocked(container *di.Container, defs []Definition) ([]actionMeta, error) {
	names := make(map[string]struct{}, len(r.handlers)+len(r.typed)+len(defs))
	for name := range r.handlers {
		names[name] = struct{}{}
	}
	for name := range r.typed {
		names[name] = struct{}{}
	}
	keys := make(map[string]string, len(r.keys)+len(defs))
	for key, name := range r.keys {
		keys[key] = name
	}

	metas := make([]actionMeta, 0, len(defs))
	for _, def := range defs {
		if def.Name == "" {
			return nil, fmt.Errorf("actions: definition requires a name")
		}
		if def.New == nil {
			return nil, fmt.Errorf("actions: definition requires a constructor")
		}
		instance := def.New()
		if instance == nil {
			return nil, fmt.Errorf("actions: constructor returned nil")
		}
		if container != nil {
			if err := container.ValidateTarget(instance); err != nil {
				return nil, fmt.Errorf("actions: invalid handler %q: %w", def.Name, err)
			}
		}
		if _, exists := names[def.Name]; exists {
			return nil, fmt.Errorf("actions: handler %q already registered", def.Name)
		}
		names[def.Name] = struct{}{}
		if def.Key != "" {
			if existing, exists := keys[def.Key]; exists {
				return nil, fmt.Errorf("actions: key %q already bound to %q", def.Key, existing)
			}
			keys[def.Key] = def.Name
		}
		metas = append(metas, actionMeta{name: def.Name, key: def.Key, new: def.New})
	}

	return metas, nil
}

func (r *Router) register(meta actionMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.container != nil {
		instance := meta.new()
		if instance == nil {
			return fmt.Errorf("actions: constructor returned nil")
		}
		if err := r.container.ValidateTarget(instance); err != nil {
			return fmt.Errorf("actions: invalid handler %q: %w", meta.name, err)
		}
	}
	if _, ok := r.handlers[meta.name]; ok {
		return fmt.Errorf("actions: handler %q already registered", meta.name)
	}
	if _, ok := r.typed[meta.name]; ok {
		return fmt.Errorf("actions: handler %q already registered", meta.name)
	}
	if meta.key != "" {
		if existing, ok := r.keys[meta.key]; ok {
			return fmt.Errorf("actions: key %q already bound to %q", meta.key, existing)
		}
		r.keys[meta.key] = meta.name
	}
	r.handlers[meta.name] = meta
	return nil
}

// Validate checks every registered declarative action against a container.
func (r *Router) Validate(container *di.Container) error {
	r.mu.RLock()
	metas := make([]actionMeta, 0, len(r.handlers))
	for _, meta := range r.handlers {
		metas = append(metas, meta)
	}
	r.mu.RUnlock()

	for _, meta := range metas {
		instance := meta.new()
		if instance == nil {
			return fmt.Errorf("actions: constructor for %q returned nil", meta.name)
		}
		if err := container.ValidateTarget(instance); err != nil {
			return fmt.Errorf("actions: invalid handler %q: %w", meta.name, err)
		}
	}
	return nil
}

// Dispatch executes an action by name.
func (r *Router) Dispatch(ctx context.Context, name string, payload ...any) (any, error) {
	r.mu.RLock()
	meta, ok := r.handlers[name]
	typed := r.typed[name]
	container := r.container
	bus := r.bus
	asyncEvents := r.asyncEvents
	r.mu.RUnlock()

	if !ok && typed == nil {
		return nil, fmt.Errorf("actions: no handler for %q", name)
	}
	if typed != nil {
		if len(payload) != 1 {
			return nil, fmt.Errorf("actions: typed action %q requires one payload", name)
		}
		return typed(ctx, payload[0])
	}

	if container == nil {
		var err error
		container, err = r.Build()
		if err != nil {
			return nil, err
		}
	}

	instance := newAction(meta)
	if err := container.Inject(instance); err != nil {
		return nil, fmt.Errorf("actions: inject %q: %w", name, err)
	}
	if len(payload) > 0 && payload[0] != nil {
		if err := bindPayload(instance, payload[0]); err != nil {
			return nil, err
		}
	}

	result, err := instance.Handle(ctx)
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
	if ok {
		return true
	}
	_, ok = r.typed[name]
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
	for name := range r.typed {
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
		name: name,
		key:  key,
		new: func() Handler {
			newVal := reflect.New(typ).Elem()
			newVal.Set(val.Elem())
			return newVal.Addr().Interface().(Handler)
		},
	}
}

// HandlerName returns the action name declared by a prototype.
func HandlerName(prototype Handler) (string, error) {
	value := reflect.ValueOf(prototype)
	if !value.IsValid() || value.Kind() != reflect.Ptr || value.IsNil() ||
		value.Elem().Kind() != reflect.Struct {
		return "", fmt.Errorf("actions: prototype must be a non-nil pointer to a struct")
	}
	typ := value.Elem().Type()
	for index := 0; index < typ.NumField(); index++ {
		if name := typ.Field(index).Tag.Get("action"); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("actions: struct must have an action tag")
}

func newAction(meta actionMeta) Handler {
	return meta.new()
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
			if !field.IsExported() {
				continue
			}
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

	nilValue := !value.IsValid() || value.Kind() == reflect.Interface && value.IsNil()
	if !nilValue && value.Kind() == reflect.Interface {
		value = value.Elem()
	}

	for i := 0; i < dst.NumField(); i++ {
		fieldMeta := dstType.Field(i)
		field := dst.Field(i)
		if _, injected := fieldMeta.Tag.Lookup("inject"); injected {
			continue
		}
		if !field.CanSet() {
			continue
		}
		if !fieldNameMatches(fieldMeta, name) {
			continue
		}
		if nilValue {
			switch field.Kind() {
			case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
				reflect.Ptr, reflect.Slice:
				field.SetZero()
				return nil
			default:
				return fmt.Errorf("actions: cannot bind nil to %s", fieldMeta.Name)
			}
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
	return fmt.Errorf("actions: unknown payload field %q", name)
}

func fieldNameMatches(field reflect.StructField, name string) bool {
	if tag := field.Tag.Get("json"); tag != "" {
		if strings.EqualFold(strings.Split(tag, ",")[0], name) {
			return true
		}
	}
	return false
}

func emitAction(ctx context.Context, bus *events.Bus, async bool, event any) error {
	if bus == nil {
		return nil
	}
	if async {
		return events.EmitAnyAsync(ctx, bus, event)
	}
	return events.EmitAny(ctx, bus, event)
}
