package hosting

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/srv"
)

type fakeSvc struct {
	mu      sync.Mutex
	running bool
}

func (f *fakeSvc) Execute(ctx context.Context) error {
	f.mu.Lock()
	f.running = true
	f.mu.Unlock()
	<-ctx.Done()
	f.mu.Lock()
	f.running = false
	f.mu.Unlock()
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

	if !svc.running {
		t.Error("service should be running")
	}

	cancel()
	<-done
	if svc.running {
		t.Error("service should have shut down")
	}
}