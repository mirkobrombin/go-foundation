package hosting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/app/web"
	"github.com/mirkobrombin/go-foundation/v2/core/health"
)

// HostState represents the current lifecycle state of a Host.
type HostState int32

// ErrHostAlreadyRun is returned when Run is called more than once.
var ErrHostAlreadyRun = errors.New("hosting: host can only run once")

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
	Server          *web.Server
	HealthRegistry  *health.Registry
	cancel          context.CancelFunc
	mu              sync.RWMutex
	runStarted      atomic.Bool
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
	Svc        BackgroundService
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	completion chan error
	err        error
}

func (a *BackgroundServiceAdapter) Start(ctx context.Context) error {
	if a.Svc == nil {
		return fmt.Errorf("hosting: background service is nil")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return fmt.Errorf("hosting: background service already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	completion := make(chan error, 1)
	a.cancel = cancel
	a.done = done
	a.completion = completion
	go func() {
		err := safeLifecycleCall("background service execute", func() error {
			return a.Svc.Execute(runCtx)
		})
		a.mu.Lock()
		a.err = err
		a.mu.Unlock()
		completion <- err
		close(completion)
		close(done)
	}()
	return nil
}

// Completion reports when the adapted background service exits.
func (a *BackgroundServiceAdapter) Completion() <-chan error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.completion
}

func (a *BackgroundServiceAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	a.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		a.mu.Lock()
		err := a.err
		a.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Host) Run(ctx context.Context) error {
	if !h.runStarted.CompareAndSwap(false, true) {
		return ErrHostAlreadyRun
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	h.mu.Lock()
	h.cancel = cancel
	h.mu.Unlock()

	h.state.Store(int32(HostStarting))

	for _, fn := range h.onStart {
		if err := safeLifecycleCall("on-start callback", func() error {
			fn()
			return nil
		}); err != nil {
			cancel()
			h.state.Store(int32(HostStopping))
			cleanupDone := make(chan error, 1)
			go func() {
				cleanupDone <- h.finishShutdown()
			}()
			select {
			case cleanupErr := <-cleanupDone:
				return errors.Join(err, cleanupErr)
			case <-time.After(h.shutdownTimeout()):
				return errors.Join(
					err,
					fmt.Errorf("host shutdown timed out after %s", h.shutdownTimeout()),
				)
			}
		}
	}

	startupCtx, startupCancel := context.WithTimeout(ctx, h.startupTimeout)
	defer startupCancel()

	startedHosted := 0
	for _, svc := range h.hostedServices {
		startResult := make(chan error, 1)
		go func(service HostedService) {
			startResult <- safeLifecycleCall("hosted service start", func() error {
				return service.Start(ctx)
			})
		}(svc)

		select {
		case startErr := <-startResult:
			if startErr != nil {
				startupCancel()
				cancel()
				h.state.Store(int32(HostStopping))
				cleanupDone := make(chan error, 1)
				go func() {
					stopErr := <-h.beginHostedShutdown(startedHosted)
					cleanupDone <- errors.Join(stopErr, h.finishShutdown())
				}()
				select {
				case cleanupErr := <-cleanupDone:
					return errors.Join(fmt.Errorf("hosted service start failed: %w", startErr), cleanupErr)
				case <-time.After(h.shutdownTimeout()):
					return errors.Join(
						fmt.Errorf("hosted service start failed: %w", startErr),
						fmt.Errorf("host shutdown timed out after %s", h.shutdownTimeout()),
					)
				}
			}
			startedHosted++
		case <-startupCtx.Done():
			startErr := startupCtx.Err()
			startupCancel()
			cancel()
			h.state.Store(int32(HostStopping))
			cleanupDone := make(chan error, 1)
			go func() {
				stopErr := <-h.beginHostedShutdown(startedHosted)
				lateErr := <-startResult
				if lateErr == nil {
					stopErr = errors.Join(stopErr, h.stopHostedService(svc))
				}
				cleanupDone <- errors.Join(lateErr, stopErr, h.finishShutdown())
			}()
			select {
			case cleanupErr := <-cleanupDone:
				return errors.Join(fmt.Errorf("hosted service start failed: %w", startErr), cleanupErr)
			case <-time.After(h.shutdownTimeout()):
				return errors.Join(
					fmt.Errorf("hosted service start failed: %w", startErr),
					fmt.Errorf("host startup cleanup timed out after %s", h.shutdownTimeout()),
				)
			}
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(h.services)+len(h.hostedServices))

	for i := 0; i < startedHosted; i++ {
		service, ok := h.hostedServices[i].(interface{ Completion() <-chan error })
		if !ok {
			continue
		}
		completion := service.Completion()
		if completion == nil {
			continue
		}
		go func() {
			select {
			case err := <-completion:
				if err == nil {
					err = errors.New("hosted background service stopped unexpectedly")
				}
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
			case <-ctx.Done():
			}
		}()
	}

	for _, svc := range h.services {
		s := svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := safeLifecycleCall("background service execute", func() error {
				return s.Execute(ctx)
			})
			if err == nil {
				if ctx.Err() != nil {
					return
				}
				err = errors.New("background service stopped unexpectedly")
			}
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}()
	}

	h.state.Store(int32(HostRunning))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var firstErr error
	select {
	case <-sigCh:
	case <-ctx.Done():
	case err := <-errCh:
		firstErr = err
	}

	cancel()

	h.state.Store(int32(HostStopping))

	cleanupDone := make(chan error, 1)
	hostedDone := h.beginHostedShutdown(startedHosted)
	go func() {
		stopErr := <-hostedDone
		wg.Wait()
		cleanupDone <- errors.Join(stopErr, h.finishShutdown())
	}()

	timeout := h.shutdownTimeout()

	select {
	case stopErr := <-cleanupDone:
		return errors.Join(firstErr, stopErr)
	case <-time.After(timeout):
		return errors.Join(
			firstErr,
			fmt.Errorf("host shutdown timed out after %s", timeout),
		)
	}
}

func (h *Host) stopHostedService(service HostedService) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), h.shutdownTimeout())
	defer cancel()
	if err := safeLifecycleCall("hosted service stop", func() error {
		return service.Stop(stopCtx)
	}); err != nil {
		return fmt.Errorf("hosted service stop failed: %w", err)
	}
	return nil
}

func (h *Host) beginHostedShutdown(started int) <-chan error {
	done := make(chan error, 1)
	go func() {
		var errs []error
		for i := started - 1; i >= 0; i-- {
			if err := h.stopHostedService(h.hostedServices[i]); err != nil {
				errs = append(errs, err)
			}
		}
		done <- errors.Join(errs...)
	}()
	return done
}

func (h *Host) shutdownTimeout() time.Duration {
	if h.ShutdownTimeout > 0 {
		return h.ShutdownTimeout
	}
	return 30 * time.Second
}

func (h *Host) finishShutdown() error {
	var errs []error
	for _, fn := range h.onStop {
		if err := safeLifecycleCall("on-stop callback", func() error {
			fn()
			return nil
		}); err != nil {
			errs = append(errs, err)
		}
	}
	if err := safeLifecycleCall("container close", h.closeContainer); err != nil {
		errs = append(errs, err)
	}
	h.state.Store(int32(HostStopped))
	return errors.Join(errs...)
}

func (h *Host) closeContainer() error {
	if h.Container == nil {
		return nil
	}
	return h.Container.Close()
}

func safeLifecycleCall(name string, call func() error) (err error) {
	if call == nil {
		return fmt.Errorf("hosting: %s is nil", name)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("hosting: %s panic: %v", name, recovered)
		}
	}()
	return call()
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
	web             *web.Server
	webAddr         string
	tlsCertFile     string
	tlsKeyFile      string
	shutdownTimeout time.Duration
	startupTimeout  time.Duration
	healthRegistry  *health.Registry
	container       *di.Container
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

func (b *HostBuilder) ConfigureWeb(fn func(*web.Server)) *HostBuilder {
	if b.web == nil {
		b.web = web.New()
	}
	fn(b.web)
	return b
}

// UseWeb attaches an existing web server to the host.
func (b *HostBuilder) UseWeb(server *web.Server) *HostBuilder {
	b.web = server
	return b
}

// UseContainer attaches an existing dependency container to the host.
func (b *HostBuilder) UseContainer(container *di.Container) *HostBuilder {
	b.container = container
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

// WithTLS configures the web host to serve HTTPS with the given certificate and key.
func (b *HostBuilder) WithTLS(certFile, keyFile string) *HostBuilder {
	if certFile == "" || keyFile == "" {
		panic("hosting: TLS certificate and key files are required")
	}
	b.tlsCertFile = certFile
	b.tlsKeyFile = keyFile
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
		Container:       b.container,
	}

	if b.web != nil {
		addr := b.webAddr
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		s := &webService{
			server:   b.web,
			addr:     addr,
			certFile: b.tlsCertFile,
			keyFile:  b.tlsKeyFile,
		}
		h.services = append(h.services, s)
		h.Server = b.web
	}

	if h.HealthRegistry != nil && h.Server != nil {
		if err := h.Server.RegisterRoutes(
			web.RouteDefinition{
				Method: http.MethodGet,
				Path:   "/health/live",
				Handler: func(ctx *web.Context) error {
					return ctx.JSON(200, map[string]string{"status": "alive"})
				},
			},
			web.RouteDefinition{
				Method: http.MethodGet,
				Path:   "/health/ready",
				Handler: func(ctx *web.Context) error {
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
				},
			},
		); err != nil {
			panic(fmt.Errorf("hosting: register health endpoints: %w", err))
		}
	}
	if b.di != nil {
		if h.Container != nil {
			panic("hosting: container already configured")
		}
		c, err := b.di.Build()
		if err != nil {
			panic(err)
		}
		h.Container = c
	}

	return h
}

type webService struct {
	server   *web.Server
	addr     string
	certFile string
	keyFile  string
}

func (w *webService) Execute(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if w.certFile != "" {
			errCh <- w.server.ListenAndServeTLS(w.addr, w.certFile, w.keyFile)
			return
		}
		errCh <- w.server.ListenAndServe(w.addr)
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-w.server.Started():
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
