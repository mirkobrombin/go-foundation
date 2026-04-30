package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/dispatcher"
	"github.com/mirkobrombin/go-foundation/pkg/hosting"
	"github.com/mirkobrombin/go-foundation/pkg/scheduler"
	"github.com/mirkobrombin/go-foundation/pkg/srv"
)

// Handler is the interface for declarative struct-tagged endpoints.
type Handler interface {
	Handle(ctx context.Context) (any, error)
}

// App orchestrates DI, HTTP, dispatching, and scheduling into a single entrypoint.
type App struct {
	container  *di.Container
	server     *srv.Server
	dispatch   *dispatcher.Dispatcher
	sched      *scheduler.Scheduler
	builder    *di.Builder
	logger     *slog.Logger
	handlerReg []Handler
}

// New creates a new App with default components.
func New() *App {
	return &App{
		builder:  di.NewBuilder(),
		server:   srv.New(),
		dispatch: dispatcher.New(),
		sched:    scheduler.New(),
		logger:   slog.Default(),
	}
}

// Log sets the application logger.
func (a *App) Log(logger *slog.Logger) *App {
	a.logger = logger
	return a
}

// Provide registers a named dependency for injection into handler structs.
func (a *App) Provide(name string, instance any) *App {
	a.builder.Provide(name, instance)
	return a
}

// RegisterHTTP registers a struct-tagged HTTP handler.
func (a *App) RegisterHTTP(h Handler) *App {
	a.handlerReg = append(a.handlerReg, h)
	return a
}

// RegisterAction registers a named action handler for dispatch.
func (a *App) RegisterAction(name string, handler func(ctx context.Context, payload ...any) (any, error)) *App {
	a.dispatch.Register(name, handler)
	return a
}

// Dispatch calls a named action handler.
func (a *App) Dispatch(ctx context.Context, name string, payload ...any) (any, error) {
	return a.dispatch.Dispatch(ctx, name, payload...)
}

// Schedule registers a cron job.
func (a *App) Schedule(name, cronExpr string, handler func(ctx context.Context) error) *App {
	a.sched.Register(scheduler.Job{Name: name, Cron: cronExpr, Handler: handler})
	return a
}

// Use adds middleware to the HTTP server.
func (a *App) Use(mw srv.Middleware) *App {
	a.server.Use(mw)
	return a
}

// Configure allows direct customization of the underlying srv.Server.
func (a *App) Configure(fn func(*srv.Server)) *App {
	fn(a.server)
	return a
}

// Build constructs the DI container and registers all handlers.
func (a *App) Build() (*di.Container, error) {
	container, err := a.builder.Build()
	if err != nil {
		return nil, err
	}
	a.container = container

	for _, h := range a.handlerReg {
		a.server.RegisterHandler(h, container)
	}

	return container, nil
}

// Listen starts the HTTP server and scheduler, then blocks until shutdown.
func (a *App) Listen(addr string) error {
	if a.container == nil {
		if _, err := a.Build(); err != nil {
			return fmt.Errorf("app: build failed: %w", err)
		}
	}

	if addr == "" {
		addr = ":8080"
	}

	h := hosting.NewBuilder().
		WithAddr(addr).
		AddHostedService(&schedulerHost{sched: a.sched}).
		Build()

	h.Server = a.server

	return h.Run(context.Background())
}

type schedulerHost struct {
	sched *scheduler.Scheduler
}

func (s *schedulerHost) Start(ctx context.Context) error {
	go s.sched.Start(ctx)
	return nil
}

func (s *schedulerHost) Stop(ctx context.Context) error {
	return s.sched.Stop(ctx)
}