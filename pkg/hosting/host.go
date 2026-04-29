package hosting

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/srv"
)

// Host manages the lifecycle of the application: startup, running state, and
// graceful shutdown. It coordinates one or more BackgroundService instances.
//
// When ConfigureServices and/or ConfigureWeb are used, the Host also owns the
// DI container and the web server.
type Host struct {
	services  []BackgroundService
	onStart   []func()
	onStop    []func()
	Container *di.Container
	Server    *srv.Server
	cancel    context.CancelFunc
	mu        sync.RWMutex
}

// BackgroundService is the interface that every long-running service must
// implement. The Execute method is called when the host starts and the
// service is expected to run until ctx is cancelled.
//
// Example:
//
//	type MyService struct{}
//	func (s *MyService) Execute(ctx context.Context) error {
//	    <-ctx.Done()
//	    return nil
//	}
type BackgroundService interface {
	// Execute runs the service. The context is cancelled when the host
	// is shutting down; the service should return promptly.
	Execute(ctx context.Context) error
}

// Run starts all registered services and blocks until a shutdown signal
// (SIGINT / SIGTERM) is received or until all services return.
func (h *Host) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	
	h.mu.Lock()
	h.cancel = cancel
	h.mu.Unlock()

	for _, fn := range h.onStart {
		fn()
	}

	errCh := make(chan error, len(h.services))
	for _, svc := range h.services {
		s := svc
		go func() {
			if err := s.Execute(ctx); err != nil {
				errCh <- err
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
	case <-ctx.Done():
	case err := <-errCh:
		cancel()
		for _, fn := range h.onStop {
			fn()
		}
		return err
	}

	cancel()
	for _, fn := range h.onStop {
		fn()
	}
	return nil
}

// Shutdown triggers a graceful shutdown by cancelling the host context.
func (h *Host) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	cancel := h.cancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// OnStart registers a lifecycle callback invoked before services start.
func (h *Host) OnStart(fn func()) {
	h.onStart = append(h.onStart, fn)
}

// OnStop registers a lifecycle callback invoked after services stop.
func (h *Host) OnStop(fn func()) {
	h.onStop = append(h.onStop, fn)
}

// HostBuilder provides a fluent API for constructing a Host.
type HostBuilder struct {
	services []BackgroundService
	onStart  []func()
	onStop   []func()
	di       *di.Builder
	web      *srv.Server
	webAddr  string
}

// NewBuilder creates a new HostBuilder.
func NewBuilder() *HostBuilder {
	return &HostBuilder{}
}

// ConfigureServices registers typed services in a DI Builder. The container
// is built automatically and attached to the Host after Build().
func (b *HostBuilder) ConfigureServices(fn func(*di.Builder)) *HostBuilder {
	if b.di == nil {
		b.di = di.NewBuilder()
	}
	fn(b.di)
	return b
}

// ConfigureWeb registers routes and middleware on the default srv.Server.
// The server is started automatically as a BackgroundService when Run() is called.
func (b *HostBuilder) ConfigureWeb(fn func(*srv.Server)) *HostBuilder {
	if b.web == nil {
		b.web = srv.New()
	}
	fn(b.web)
	return b
}

// AddService registers a background service.
func (b *HostBuilder) AddService(svc BackgroundService) *HostBuilder {
	b.services = append(b.services, svc)
	return b
}

// OnStart registers a lifecycle callback that runs before services start.
func (b *HostBuilder) OnStart(fn func()) *HostBuilder {
	b.onStart = append(b.onStart, fn)
	return b
}

// OnStop registers a lifecycle callback that runs after services stop.
func (b *HostBuilder) OnStop(fn func()) *HostBuilder {
	b.onStop = append(b.onStop, fn)
	return b
}

// WithAddr sets the listen address for the auto-started web server
// (default: ":8080").
func (b *HostBuilder) WithAddr(addr string) *HostBuilder {
	b.webAddr = addr
	return b
}

// Build constructs a Host from the builder configuration.
func (b *HostBuilder) Build() *Host {
	h := &Host{
		services: append([]BackgroundService{}, b.services...),
		onStart:  b.onStart,
		onStop:   b.onStop,
	}

	if b.di != nil {
		h.Container = b.di.Build()
	}
	if b.web != nil {
		addr := b.webAddr
		if addr == "" {
			addr = ":8080"
		}
		s := &webService{server: b.web, container: h.Container, addr: addr}
		h.services = append(h.services, s)
		h.Server = b.web
	}

	return h
}

type webService struct {
	server    *srv.Server
	container *di.Container
	addr      string
}

func (w *webService) Execute(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.server.ListenAndServe(w.addr)
	}()
	select {
	case <-ctx.Done():
		w.server.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		return err
	}
}