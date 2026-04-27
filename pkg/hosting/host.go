package hosting

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Host manages the lifecycle of the application: startup, running state, and
// graceful shutdown. It coordinates one or more BackgroundService instances.
//
// Example:
//
//	host := hosting.NewBuilder().AddService(MyService{}).Build()
//	if err := host.Run(ctx); err != nil {
//	    log.Fatal(err)
//	}
type Host struct {
	services []BackgroundService
	onStart  []func()
	onStop   []func()
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

// OnStart registers a lifecycle callback invoked before services start.
func (h *Host) OnStart(fn func()) {
	h.onStart = append(h.onStart, fn)
}

// OnStop registers a lifecycle callback invoked after services stop.
func (h *Host) OnStop(fn func()) {
	h.onStop = append(h.onStop, fn)
}

// HostBuilder provides a fluent API for constructing a Host.
//
// Example:
//
//	host := hosting.NewBuilder().
//	    AddService(MyService{}).
//	    OnStart(func() { log.Println("starting") }).
//	    Build()
type HostBuilder struct {
	services []BackgroundService
	onStart  []func()
	onStop   []func()
}

// NewBuilder creates a new HostBuilder.
func NewBuilder() *HostBuilder {
	return &HostBuilder{}
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

// Build constructs a Host from the builder configuration.
func (b *HostBuilder) Build() *Host {
	return &Host{
		services: b.services,
		onStart:  b.onStart,
		onStop:   b.onStop,
	}
}
