package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mirkobrombin/go-foundation/v2/app/actions"
	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/app/dispatcher"
	"github.com/mirkobrombin/go-foundation/v2/app/hosting"
	"github.com/mirkobrombin/go-foundation/v2/app/web"
	"github.com/mirkobrombin/go-foundation/v2/core/events"
	"github.com/mirkobrombin/go-foundation/v2/core/scheduler"
)

// Handler is the interface for declarative struct-tagged endpoints.
type Handler = web.Handler

// App orchestrates DI, HTTP, dispatching, and scheduling into a single entrypoint.
type App struct {
	container  *di.Container
	server     *web.Server
	actions    *actions.Router
	dispatch   *dispatcher.Dispatcher
	sched      *scheduler.Scheduler
	builder    *di.Builder
	logger     *slog.Logger
	handlerReg []Handler
	handlerDef []web.HandlerDefinition
	actionDef  []actions.Definition
}

// New creates a new App with default components.
func New() *App {
	app := &App{
		builder:  di.NewBuilder(),
		server:   web.New(),
		actions:  actions.New(),
		dispatch: dispatcher.New(),
		sched:    scheduler.New(),
	}
	return app.Log(slog.Default())
}

// Log sets the application logger.
func (a *App) Log(logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	a.logger = logger
	a.sched.SetLogger(func(message string) {
		logger.Info(message)
	})
	return a
}

// Provide registers a named dependency for injection into handler structs.
func (a *App) Provide(name string, instance any) *App {
	if a.container != nil {
		a.container.Provide(name, instance)
		return a
	}
	a.builder.Provide(name, instance)
	a.actions.Provide(name, instance)
	return a
}

// RegisterHTTP registers a struct-tagged HTTP handler.
func (a *App) RegisterHTTP(h Handler) *App {
	if a.container != nil {
		if err := a.server.RegisterHandler(h, a.container); err != nil {
			panic(err)
		}
		return a
	}
	a.handlerReg = append(a.handlerReg, h)
	return a
}

// RegisterHTTPDefinition registers a statically described HTTP handler.
func (a *App) RegisterHTTPDefinition(def web.HandlerDefinition) *App {
	if a.container != nil {
		if err := a.server.RegisterDefinition(def, a.container); err != nil {
			panic(err)
		}
		return a
	}
	a.handlerDef = append(a.handlerDef, def)
	return a
}

// RegisterHTTPDefinitions registers statically described HTTP handlers.
func (a *App) RegisterHTTPDefinitions(defs ...web.HandlerDefinition) *App {
	if a.container != nil {
		if err := a.server.RegisterDefinitions(a.container, defs...); err != nil {
			panic(err)
		}
		return a
	}
	a.handlerDef = append(a.handlerDef, defs...)
	return a
}

// RegisterAction registers a named action handler for dispatch.
func (a *App) RegisterAction(name string, handler func(ctx context.Context, payload ...any) (any, error)) *App {
	if a.actions.Has(name) {
		panic(fmt.Sprintf("app: action %q is registered in both action routers", name))
	}
	a.dispatch.Register(name, handler)
	return a
}

// RegisterActionHandler registers a struct-tagged action handler.
func (a *App) RegisterActionHandler(h actions.Handler) *App {
	name, err := actions.HandlerName(h)
	if err != nil {
		panic(err)
	}
	if a.dispatch.Has(name) {
		panic(fmt.Sprintf("app: action %q is registered in both action routers", name))
	}
	a.actions.Register(h)
	return a
}

// RegisterActionDefinition registers a statically described action.
func (a *App) RegisterActionDefinition(def actions.Definition) *App {
	if a.dispatch.Has(def.Name) {
		panic(fmt.Sprintf("app: action %q is registered in both action routers", def.Name))
	}
	if a.container != nil {
		if err := a.actions.RegisterDefinition(def); err != nil {
			panic(err)
		}
		return a
	}
	a.actionDef = append(a.actionDef, def)
	return a
}

// RegisterActionDefinitions registers statically described actions.
func (a *App) RegisterActionDefinitions(defs ...actions.Definition) *App {
	for _, def := range defs {
		if a.dispatch.Has(def.Name) {
			panic(fmt.Sprintf("app: action %q is registered in both action routers", def.Name))
		}
	}
	if a.container != nil {
		if err := a.actions.RegisterDefinitions(defs...); err != nil {
			panic(err)
		}
		return a
	}
	a.actionDef = append(a.actionDef, defs...)
	return a
}

// Dispatch calls a named action handler.
func (a *App) Dispatch(ctx context.Context, name string, payload ...any) (any, error) {
	if a.dispatch.Has(name) {
		return a.dispatch.Dispatch(ctx, name, payload...)
	}
	if a.container == nil {
		if _, err := a.Build(); err != nil {
			return nil, fmt.Errorf("app: build failed: %w", err)
		}
	}
	return a.actions.Dispatch(ctx, name, payload...)
}

// DispatchKey calls a struct-tagged action handler by key binding.
func (a *App) DispatchKey(ctx context.Context, key string, payload ...any) (any, error) {
	if a.container == nil {
		if _, err := a.Build(); err != nil {
			return nil, fmt.Errorf("app: build failed: %w", err)
		}
	}
	return a.actions.DispatchKey(ctx, key, payload...)
}

// UseActionEvents emits action instances after dispatch.
func (a *App) UseActionEvents(bus *events.Bus) *App {
	a.actions.UseEvents(bus)
	return a
}

// UseAsyncActionEvents emits action instances asynchronously after dispatch.
func (a *App) UseAsyncActionEvents(bus *events.Bus) *App {
	a.actions.UseAsyncEvents(bus)
	return a
}

// Actions returns registered struct-tagged action names.
func (a *App) Actions() []string {
	return a.actions.Actions()
}

// KeyBindings returns struct-tagged action key bindings.
func (a *App) KeyBindings() map[string]string {
	return a.actions.KeyBindings()
}

// Schedule registers a cron job.
func (a *App) Schedule(name, cronExpr string, handler func(ctx context.Context) error) *App {
	if err := a.sched.Register(scheduler.Job{Name: name, Cron: cronExpr, Handler: handler}); err != nil {
		panic(err)
	}
	return a
}

// Use adds middleware to the HTTP server.
func (a *App) Use(mw web.Middleware) *App {
	a.server.Use(mw)
	return a
}

// Configure allows direct customization of the underlying web.Server.
func (a *App) Configure(fn func(*web.Server)) *App {
	fn(a.server)
	return a
}

// Build constructs the DI container and registers all handlers.
func (a *App) Build() (*di.Container, error) {
	if a.container != nil {
		return a.container, nil
	}
	container, err := a.builder.Build()
	if err != nil {
		return nil, err
	}
	for _, name := range a.actions.Actions() {
		if a.dispatch.Has(name) {
			return nil, fmt.Errorf("app: action %q is registered in both action routers", name)
		}
	}
	for _, definition := range a.actionDef {
		if a.dispatch.Has(definition.Name) {
			return nil, fmt.Errorf("app: action %q is registered in both action routers", definition.Name)
		}
	}
	if err := a.actions.Validate(container); err != nil {
		return nil, err
	}
	if err := a.actions.ValidateDefinitions(container, a.actionDef...); err != nil {
		return nil, err
	}

	httpDefinitions := append([]web.HandlerDefinition(nil), a.handlerDef...)
	for _, prototype := range a.handlerReg {
		definition, err := web.DefinitionFromHandler(prototype)
		if err != nil {
			return nil, err
		}
		httpDefinitions = append(httpDefinitions, definition)
	}
	if err := a.server.ValidateDefinitions(container, httpDefinitions...); err != nil {
		return nil, err
	}

	a.actions.UseContainer(container)
	if err := a.actions.RegisterDefinitions(a.actionDef...); err != nil {
		return nil, err
	}
	if err := a.server.RegisterDefinitions(container, httpDefinitions...); err != nil {
		return nil, err
	}

	a.actionDef = nil
	a.handlerReg = nil
	a.handlerDef = nil
	a.container = container
	return container, nil
}

// Listen starts the HTTP server and scheduler, then blocks until shutdown.
func (a *App) Listen(addr string) error {
	return a.listen(addr, "", "")
}

// ListenTLS starts HTTPS using the given certificate and key files.
func (a *App) ListenTLS(addr, certFile, keyFile string) error {
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("app: TLS certificate and key files are required")
	}
	return a.listen(addr, certFile, keyFile)
}

func (a *App) listen(addr, certFile, keyFile string) error {
	if a.container == nil {
		if _, err := a.Build(); err != nil {
			return fmt.Errorf("app: build failed: %w", err)
		}
	}

	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	builder := hosting.NewBuilder().
		WithAddr(addr).
		UseContainer(a.container).
		UseWeb(a.server).
		AddHostedService(&schedulerHost{sched: a.sched})
	if certFile != "" {
		builder.WithTLS(certFile, keyFile)
	}
	h := builder.Build()

	return h.Run(context.Background())
}

type schedulerHost struct {
	sched *scheduler.Scheduler
}

func (s *schedulerHost) Start(ctx context.Context) error {
	return s.sched.Start(ctx)
}

func (s *schedulerHost) Stop(ctx context.Context) error {
	return s.sched.Stop(ctx)
}

func (s *schedulerHost) Completion() <-chan error {
	return s.sched.Completion()
}
