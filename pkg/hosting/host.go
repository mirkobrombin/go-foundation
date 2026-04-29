package hosting

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/health"
	"github.com/mirkobrombin/go-foundation/pkg/srv"
)

// HostState represents the current lifecycle state of a Host.
type HostState int32

const (
	// HostStarting indicates the host is starting hosted services.
	HostStarting HostState = iota
	// HostRunning indicates the host is fully running.
	HostRunning
	// HostStopping indicates the host is shutting down.
	HostStopping
	// HostStopped indicates the host has finished shutting down.
	HostStopped
)

func (s HostState) String() string {
	switch s {
	case HostStarting:
		return "starting"
	case HostRunning:
		return "running"
	case HostStopping:
		return "stopping"
	case HostStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Host manages the lifecycle of the application.
type Host struct {
	services        []BackgroundService
	hostedServices  []HostedService
	onStart         []func()
	onStop          []func()
	Container       *di.Container
	Server          *srv.Server
	HealthRegistry  *health.Registry
	cancel          context.CancelFunc
	mu              sync.RWMutex
	state           atomic.Int32
	ShutdownTimeout time.Duration
	startupTimeout  time.Duration
}

// BackgroundService is a long-running service started in parallel.
type BackgroundService interface {
	Execute(ctx context.Context) error
}

// HostedService is a managed lifecycle service with explicit Start/Stop.
type HostedService interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// BackgroundServiceAdapter wraps a BackgroundService as a HostedService.
type BackgroundServiceAdapter struct {
	Svc BackgroundService
}

func (a *BackgroundServiceAdapter) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.Svc.Execute(ctx)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *BackgroundServiceAdapter) Stop(_ context.Context) error {
	return nil
}

func (h *Host) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	h.mu.Lock()
	h.cancel = cancel
	h.mu.Unlock()

	h.state.Store(int32(HostStarting))

	for _, fn := range h.onStart {
		fn()
	}

	startupCtx, startupCancel := context.WithTimeout(ctx, h.startupTimeout)
	defer startupCancel()

	for _, svc := range h.hostedServices {
		if err := svc.Start(startupCtx); err != nil {
			startupCancel()
			h.shutdownHosted(ctx)
			return fmt.Errorf("hosted service start failed: %w", err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(h.services))

	for _, svc := range h.services {
		s := svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Execute(ctx); err != nil {
				errCh <- err
			}
		}()
	}

	h.state.Store(int32(HostRunning))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var firstErr error
	select {
	case <-sigCh:
	case <-ctx.Done():
	case err := <-errCh:
		firstErr = err
	}

	cancel()

	h.state.Store(int32(HostStopping))

	h.shutdownHosted(ctx)

	stopped := make(chan struct{})
	go func() {
		wg.Wait()
		close(stopped)
	}()

	timeout := h.ShutdownTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	select {
	case <-stopped:
	case <-time.After(timeout):
	}

	for _, fn := range h.onStop {
		fn()
	}

	h.state.Store(int32(HostStopped))

	if firstErr != nil {
		return firstErr
	}
	return nil
}

func (h *Host) shutdownHosted(_ context.Context) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := len(h.hostedServices) - 1; i >= 0; i-- {
		h.hostedServices[i].Stop(stopCtx)
	}
}

func (h *Host) Shutdown(_ context.Context) error {
	h.mu.Lock()
	cancel := h.cancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (h *Host) State() HostState {
	return HostState(h.state.Load())
}

func (h *Host) OnStart(fn func()) {
	h.onStart = append(h.onStart, fn)
}

func (h *Host) OnStop(fn func()) {
	h.onStop = append(h.onStop, fn)
}

func (h *Host) AddHostedService(svc HostedService) {
	h.hostedServices = append(h.hostedServices, svc)
}

// HostBuilder provides a fluent API for constructing a Host.
type HostBuilder struct {
	services        []BackgroundService
	hostedServices  []HostedService
	onStart         []func()
	onStop          []func()
	di              *di.Builder
	web            *srv.Server
	webAddr        string
	shutdownTimeout time.Duration
	startupTimeout  time.Duration
	healthRegistry *health.Registry
}

// NewBuilder creates a new HostBuilder.
func NewBuilder() *HostBuilder {
	return &HostBuilder{
		startupTimeout: 15 * time.Second,
	}
}

func (b *HostBuilder) ConfigureServices(fn func(*di.Builder)) *HostBuilder {
	if b.di == nil {
		b.di = di.NewBuilder()
	}
	fn(b.di)
	return b
}

func (b *HostBuilder) ConfigureWeb(fn func(*srv.Server)) *HostBuilder {
	if b.web == nil {
		b.web = srv.New()
	}
	fn(b.web)
	return b
}

func (b *HostBuilder) AddService(svc BackgroundService) *HostBuilder {
	b.services = append(b.services, svc)
	return b
}

func (b *HostBuilder) AddHostedService(svc HostedService) *HostBuilder {
	b.hostedServices = append(b.hostedServices, svc)
	return b
}

func (b *HostBuilder) OnStart(fn func()) *HostBuilder {
	b.onStart = append(b.onStart, fn)
	return b
}

func (b *HostBuilder) OnStop(fn func()) *HostBuilder {
	b.onStop = append(b.onStop, fn)
	return b
}

func (b *HostBuilder) WithAddr(addr string) *HostBuilder {
	b.webAddr = addr
	return b
}

func (b *HostBuilder) WithShutdownTimeout(d time.Duration) *HostBuilder {
	b.shutdownTimeout = d
	return b
}

func (b *HostBuilder) WithStartupTimeout(d time.Duration) *HostBuilder {
	b.startupTimeout = d
	return b
}

func (b *HostBuilder) WithHealthRegistry(r *health.Registry) *HostBuilder {
	b.healthRegistry = r
	return b
}

func (b *HostBuilder) Build() *Host {
	h := &Host{
		services:        append([]BackgroundService{}, b.services...),
		hostedServices:  append([]HostedService{}, b.hostedServices...),
		onStart:         b.onStart,
		onStop:          b.onStop,
		ShutdownTimeout: b.shutdownTimeout,
		startupTimeout:  b.startupTimeout,
		HealthRegistry:  b.healthRegistry,
	}

	if b.di != nil {
		c, err := b.di.Build()
		if err != nil {
			panic(err)
		}
		h.Container = c
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

	if h.HealthRegistry != nil && h.Server != nil {
		h.Server.MapGet("/health/live", func(ctx *srv.Context) error {
			return ctx.JSON(200, map[string]string{"status": "alive"})
		})
		h.Server.MapGet("/health/ready", func(ctx *srv.Context) error {
			results := h.HealthRegistry.CheckAll(ctx.Request.Context())
			healthy := true
			details := make(map[string]string, len(results))
			for name, report := range results {
				details[name] = report.Status.String()
				if report.Status == health.StatusUnhealthy {
					healthy = false
				}
			}
			code := 200
			status := "ready"
			if !healthy {
				code = 503
				status = "not ready"
			}
			return ctx.JSON(code, map[string]any{
				"status":  status,
				"details": details,
			})
		})
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