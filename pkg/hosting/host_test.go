package hosting

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/health"
	"github.com/mirkobrombin/go-foundation/pkg/srv"
)

type fakeSvc struct {
	running atomic.Bool
}

func (f *fakeSvc) Execute(ctx context.Context) error {
	f.running.Store(true)
	<-ctx.Done()
	f.running.Store(false)
	return nil
}

func TestBuilder_ConfigureServices(t *testing.T) {
	h := NewBuilder().
		ConfigureServices(func(b *di.Builder) {
			di.RegisterInstance[*fakeSvc](b, &fakeSvc{})
		}).
		Build()

	if h.Container == nil {
		t.Fatal("Container should not be nil")
	}
	svc := di.ResolveType[*fakeSvc](h.Container)
	if svc == nil {
		t.Fatal("should resolve fakeSvc")
	}
}

func TestBuilder_ConfigureWeb(t *testing.T) {
	h := NewBuilder().
		ConfigureWeb(func(app *srv.Server) {
			app.MapGet("/test", func(c *srv.Context) error {
				c.String(200, "ok")
				return nil
			})
		}).
		WithAddr(":0").
		Build()

	if h.Server == nil {
		t.Fatal("Server should not be nil")
	}
	if len(h.services) != 1 {
		t.Fatalf("expected 1 service (web), got %d", len(h.services))
	}
}

func TestBuilder_AddService(t *testing.T) {
	svc := &fakeSvc{}
	host := NewBuilder().AddService(svc).Build()
	if len(host.services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(host.services))
	}
}

func TestBuilder_WithAddr(t *testing.T) {
	b := NewBuilder().WithAddr(":1234")
	if b.webAddr != ":1234" {
		t.Errorf("addr: got %q, want :1234", b.webAddr)
	}
}

func TestHost_Lifecycle(t *testing.T) {
	svc := &fakeSvc{}
	host := NewBuilder().AddService(svc).Build()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		host.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	if !svc.running.Load() {
		t.Error("service should be running")
	}

	cancel()
	<-done
	time.Sleep(20 * time.Millisecond)
	if svc.running.Load() {
		t.Error("service should have shut down")
	}
}

// --- M7 tests ---

type fakeHosted struct {
	started atomic.Bool
	stopped atomic.Bool
	startErr error
}

func (f *fakeHosted) Start(_ context.Context) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started.Store(true)
	return nil
}

func (f *fakeHosted) Stop(_ context.Context) error {
	f.stopped.Store(true)
	return nil
}

func TestHostedService_Lifecycle(t *testing.T) {
	svc := &fakeHosted{}
	host := NewBuilder().AddHostedService(svc).Build()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		host.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	if !svc.started.Load() {
		t.Error("hosted service should be started")
	}

	cancel()
	<-done

	if !svc.stopped.Load() {
		t.Error("hosted service should be stopped")
	}
}

func TestHostedService_StartupFailure(t *testing.T) {
	failSvc := &fakeHosted{startErr: context.DeadlineExceeded}
	host := NewBuilder().AddHostedService(failSvc).Build()

	err := host.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from startup failure")
	}
}

func TestHost_StartupTimeout(t *testing.T) {
	slowSvc := &slowHosted{}
	host := NewBuilder().
		AddHostedService(slowSvc).
		WithStartupTimeout(100*time.Millisecond).
		Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- host.Run(ctx)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to return")
	}
}

type slowHosted struct{}

func (s *slowHosted) Start(ctx context.Context) error {
	select {
	case <-time.After(5 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *slowHosted) Stop(_ context.Context) error { return nil }

func TestHost_State(t *testing.T) {
	host := NewBuilder().Build()
	if host.State() != HostStarting {
		t.Errorf("initial state = %v, want Starting", host.State())
	}
}

func TestHost_AddHostedService(t *testing.T) {
	svc := &fakeHosted{}
	host := &Host{}
	host.AddHostedService(svc)
	if len(host.hostedServices) != 1 {
		t.Fatalf("expected 1 hosted service, got %d", len(host.hostedServices))
	}
}

func TestBackgroundServiceAdapter(t *testing.T) {
	fake := &fakeSvc{}
	adapter := &BackgroundServiceAdapter{Svc: fake}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- adapter.Start(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	err := <-errCh
	if err != nil && err != context.Canceled {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHost_WithHealthRegistry_ReadyEndpoint(t *testing.T) {
	reg := health.NewRegistry()
	reg.Register("db", &testChecker{status: health.StatusHealthy})

	h := NewBuilder().
		ConfigureWeb(func(s *srv.Server) {}).
		WithHealthRegistry(reg).
		WithAddr(":0").
		Build()

	if h.Server == nil {
		t.Fatal("Server should be set")
	}
}

type testChecker struct {
	status health.Status
}

func (c *testChecker) Check(_ context.Context) health.Report {
	return health.Report{Status: c.status}
}

func TestHost_ShutdownTimeout(t *testing.T) {
	b := NewBuilder().WithShutdownTimeout(5 * time.Second)
	if b.shutdownTimeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", b.shutdownTimeout)
	}
}

func TestHost_DefaultStartupTimeout(t *testing.T) {
	b := NewBuilder()
	if b.startupTimeout != 15*time.Second {
		t.Errorf("default startup timeout = %v, want 15s", b.startupTimeout)
	}
}