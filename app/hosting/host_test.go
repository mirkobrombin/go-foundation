package hosting

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/app/web"
	"github.com/mirkobrombin/go-foundation/v2/core/health"
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
		ConfigureWeb(func(app *web.Server) {
			app.MapGet("/test", func(c *web.Context) error {
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

func TestBuilder_UseWeb(t *testing.T) {
	server := web.New()
	host := NewBuilder().UseWeb(server).Build()
	if host.Server != server {
		t.Fatal("UseWeb() did not attach the server")
	}
	if len(host.services) != 1 {
		t.Fatalf("host services = %d, want 1", len(host.services))
	}
}

func TestWebServiceHandlesImmediateCancellation(t *testing.T) {
	service := &webService{server: web.New(), addr: ":0"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Execute(ctx); err != nil {
		t.Fatalf("Execute() error = %v", err)
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

func TestHost_RunIsOneShot(t *testing.T) {
	svc := &fakeSvc{}
	host := NewBuilder().AddService(svc).Build()

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- host.Run(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for host.State() != HostRunning && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if host.State() != HostRunning {
		t.Fatal("first Run() did not start the host")
	}

	if err := host.Run(context.Background()); !errors.Is(err, ErrHostAlreadyRun) {
		t.Fatalf("concurrent Run() error = %v, want %v", err, ErrHostAlreadyRun)
	}

	cancel()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := host.Run(context.Background()); !errors.Is(err, ErrHostAlreadyRun) {
		t.Fatalf("second Run() error = %v, want %v", err, ErrHostAlreadyRun)
	}
}

type panicBackgroundService struct{}

func (*panicBackgroundService) Execute(context.Context) error {
	panic("background failed")
}

func TestHostRecoversBackgroundServicePanic(t *testing.T) {
	host := NewBuilder().AddService(&panicBackgroundService{}).Build()
	err := host.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "background failed") {
		t.Fatalf("Run() error = %v", err)
	}
	if host.State() != HostStopped {
		t.Fatalf("state = %s, want stopped", host.State())
	}
}

func TestHostOnStartPanicStillClosesContainer(t *testing.T) {
	closer := &trackedCloser{}
	builder := di.NewBuilder()
	di.RegisterInstance(builder, closer)
	host := NewBuilder().
		UseContainer(builder.MustBuild()).
		OnStart(func() { panic("start failed") }).
		Build()

	err := host.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("Run() error = %v", err)
	}
	if !closer.closed.Load() {
		t.Fatal("OnStart panic skipped container cleanup")
	}
}

type fakeHosted struct {
	started  atomic.Bool
	stopped  atomic.Bool
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
		WithStartupTimeout(100 * time.Millisecond).
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

type blockingHosted struct {
	release chan struct{}
	stopped atomic.Bool
}

func (s *blockingHosted) Start(context.Context) error {
	<-s.release
	return nil
}

func (s *blockingHosted) Stop(context.Context) error {
	s.stopped.Store(true)
	return nil
}

type trackedCloser struct {
	closed atomic.Bool
}

func (c *trackedCloser) Close() error {
	c.closed.Store(true)
	return nil
}

type blockingCloser struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingCloser) Close() error {
	close(c.started)
	<-c.release
	return nil
}

func TestHost_StartupTimeoutDefersCleanupUntilStartReturns(t *testing.T) {
	service := &blockingHosted{release: make(chan struct{})}
	closer := &trackedCloser{}
	builder := di.NewBuilder()
	di.RegisterInstance(builder, closer)
	container := builder.MustBuild()
	var onStop atomic.Bool
	host := NewBuilder().
		UseContainer(container).
		AddHostedService(service).
		OnStop(func() { onStop.Store(true) }).
		WithStartupTimeout(10 * time.Millisecond).
		WithShutdownTimeout(20 * time.Millisecond).
		Build()

	err := host.Run(context.Background())
	if err == nil {
		t.Fatal("Run() accepted a hosted service that ignored startup cancellation")
	}
	if host.State() != HostStopping {
		t.Fatalf("state = %s, want stopping", host.State())
	}
	if onStop.Load() || closer.closed.Load() {
		t.Fatal("host finalized while the Start call was still running")
	}

	close(service.release)
	deadline := time.Now().Add(time.Second)
	for host.State() != HostStopped && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if host.State() != HostStopped {
		t.Fatal("host did not finish deferred startup cleanup")
	}
	if !service.stopped.Load() || !onStop.Load() || !closer.closed.Load() {
		t.Fatal("deferred startup cleanup did not stop and close all resources")
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

	if err := adapter.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for !fake.running.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !fake.running.Load() {
		t.Fatal("background service did not start")
	}
	if err := adapter.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if fake.running.Load() {
		t.Fatal("background service did not stop")
	}
}

type failingBackground struct {
	err error
}

func (f *failingBackground) Execute(context.Context) error {
	return f.err
}

func TestHost_StopsWhenAdaptedBackgroundServiceFails(t *testing.T) {
	want := errors.New("background failed")
	host := NewBuilder().
		AddHostedService(&BackgroundServiceAdapter{
			Svc: &failingBackground{err: want},
		}).
		Build()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := host.Run(ctx)
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if host.State() != HostStopped {
		t.Fatalf("state = %s, want stopped", host.State())
	}
}

func TestHostRecoversAdaptedBackgroundServicePanic(t *testing.T) {
	host := NewBuilder().
		AddHostedService(&BackgroundServiceAdapter{Svc: &panicBackgroundService{}}).
		Build()

	err := host.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "background failed") {
		t.Fatalf("Run() error = %v", err)
	}
	if host.State() != HostStopped {
		t.Fatalf("state = %s, want stopped", host.State())
	}
}

func TestHost_WithHealthRegistry_ReadyEndpoint(t *testing.T) {
	reg := health.NewRegistry()
	reg.Register("db", &testChecker{status: health.StatusHealthy})

	h := NewBuilder().
		ConfigureWeb(func(s *web.Server) {}).
		WithHealthRegistry(reg).
		WithAddr(":0").
		Build()

	if h.Server == nil {
		t.Fatal("Server should be set")
	}
}

func TestHost_RejectsHealthEndpointConflict(t *testing.T) {
	server := web.New()
	if err := server.MapGet("/health/live", func(*web.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registry := health.NewRegistry()

	defer func() {
		if recover() == nil {
			t.Fatal("Build() ignored a health endpoint conflict")
		}
	}()
	NewBuilder().
		UseWeb(server).
		WithHealthRegistry(registry).
		Build()
}

func TestHost_HealthEndpointRegistrationIsTransactional(t *testing.T) {
	server := web.New()
	if err := server.MapGet("/health/ready", func(*web.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Build() ignored readiness endpoint conflict")
			}
		}()
		NewBuilder().
			UseWeb(server).
			WithHealthRegistry(health.NewRegistry()).
			Build()
	}()

	for _, route := range server.Routes() {
		if route.Path == "/health/live" {
			t.Fatal("failed health batch partially registered liveness endpoint")
		}
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

type blockingBackground struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingBackground) Execute(context.Context) error {
	close(s.started)
	<-s.release
	return nil
}

func TestHost_ShutdownTimeoutDefersCleanupUntilServicesExit(t *testing.T) {
	service := &blockingBackground{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	closer := &trackedCloser{}
	builder := di.NewBuilder()
	di.RegisterInstance(builder, closer)
	container := builder.MustBuild()
	var onStop atomic.Bool
	host := NewBuilder().
		UseContainer(container).
		AddService(service).
		OnStop(func() { onStop.Store(true) }).
		WithShutdownTimeout(20 * time.Millisecond).
		Build()
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- host.Run(ctx)
	}()
	<-service.started
	cancel()

	err := <-runDone
	if err == nil {
		t.Fatal("Run() ignored shutdown timeout")
	}
	if host.State() != HostStopping {
		t.Fatalf("state = %s, want stopping", host.State())
	}
	if onStop.Load() || closer.closed.Load() {
		t.Fatal("host finalized while a background service was still running")
	}

	close(service.release)
	deadline := time.Now().Add(time.Second)
	for host.State() != HostStopped && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if host.State() != HostStopped || !onStop.Load() || !closer.closed.Load() {
		t.Fatal("host did not finish deferred shutdown cleanup")
	}
}

type blockingStopHosted struct {
	started atomic.Bool
	release chan struct{}
}

func (s *blockingStopHosted) Start(context.Context) error {
	s.started.Store(true)
	return nil
}

func (s *blockingStopHosted) Stop(context.Context) error {
	<-s.release
	return nil
}

func TestHost_ShutdownTimeoutDefersCleanupUntilHostedStops(t *testing.T) {
	service := &blockingStopHosted{release: make(chan struct{})}
	closer := &trackedCloser{}
	builder := di.NewBuilder()
	di.RegisterInstance(builder, closer)
	container := builder.MustBuild()
	var onStop atomic.Bool
	host := NewBuilder().
		UseContainer(container).
		AddHostedService(service).
		OnStop(func() { onStop.Store(true) }).
		WithShutdownTimeout(20 * time.Millisecond).
		Build()
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- host.Run(ctx)
	}()
	deadline := time.Now().Add(time.Second)
	for !service.started.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()

	if err := <-runDone; err == nil {
		t.Fatal("Run() ignored a hosted service that blocked in Stop")
	}
	if host.State() != HostStopping {
		t.Fatalf("state = %s, want stopping", host.State())
	}
	if onStop.Load() || closer.closed.Load() {
		t.Fatal("host finalized while hosted Stop was still running")
	}

	close(service.release)
	deadline = time.Now().Add(time.Second)
	for host.State() != HostStopped && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if host.State() != HostStopped || !onStop.Load() || !closer.closed.Load() {
		t.Fatal("host did not finish deferred hosted-service cleanup")
	}
}

func TestHost_ShutdownTimeoutIncludesContainerClose(t *testing.T) {
	closer := &blockingCloser{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	builder := di.NewBuilder()
	di.RegisterInstance(builder, closer)
	host := NewBuilder().
		UseContainer(builder.MustBuild()).
		WithShutdownTimeout(20 * time.Millisecond).
		Build()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := host.Run(ctx)
	if err == nil {
		t.Fatal("Run() ignored a blocking container closer")
	}
	if host.State() != HostStopping {
		t.Fatalf("state = %s, want stopping", host.State())
	}
	select {
	case <-closer.started:
	default:
		t.Fatal("container closer was not started")
	}

	close(closer.release)
	deadline := time.Now().Add(time.Second)
	for host.State() != HostStopped && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if host.State() != HostStopped {
		t.Fatal("host did not finish after container closer returned")
	}
}

func TestHost_DefaultStartupTimeout(t *testing.T) {
	b := NewBuilder()
	if b.startupTimeout != 15*time.Second {
		t.Errorf("default startup timeout = %v, want 15s", b.startupTimeout)
	}
}

func TestHostBuilderUsesLoopbackByDefault(t *testing.T) {
	host := NewBuilder().UseWeb(web.New()).Build()
	service, ok := host.services[0].(*webService)
	if !ok {
		t.Fatalf("service type = %T, want *webService", host.services[0])
	}
	if service.addr != "127.0.0.1:8080" {
		t.Fatalf("default address = %q", service.addr)
	}
}

func TestHostBuilderConfiguresTLS(t *testing.T) {
	host := NewBuilder().
		UseWeb(web.New()).
		WithTLS("certificate.pem", "key.pem").
		Build()
	service := host.services[0].(*webService)
	if service.certFile != "certificate.pem" || service.keyFile != "key.pem" {
		t.Fatalf("TLS files = %q, %q", service.certFile, service.keyFile)
	}
}
