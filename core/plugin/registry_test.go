package plugin_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mirkobrombin/go-foundation/v2/core/plugin"
)

type testPlugin struct {
	name      string
	order     *[]string
	startFail error
	stopFail  error
}

type retryStopPlugin struct {
	name      string
	stopCalls int
}

func (p *retryStopPlugin) Name() string {
	return p.name
}

func (p *retryStopPlugin) Start() error {
	return nil
}

func (p *retryStopPlugin) Stop() error {
	p.stopCalls++
	if p.stopCalls == 1 {
		return errors.New("temporary stop failure")
	}
	return nil
}

func (p testPlugin) Name() string { return p.name }

func (p testPlugin) Start() error {
	if p.order != nil {
		*p.order = append(*p.order, "start:"+p.name)
	}
	return p.startFail
}

func (p testPlugin) Stop() error {
	if p.order != nil {
		*p.order = append(*p.order, "stop:"+p.name)
	}
	return p.stopFail
}

func TestRegistryPreservesLifecycleOrder(t *testing.T) {
	order := []string{}
	registry := plugin.NewRegistry()
	if err := registry.Register(testPlugin{name: "first", order: &order}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(testPlugin{name: "second", order: &order}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if errs := registry.StartAll(); len(errs) != 0 {
		t.Fatalf("StartAll() errors = %v, want none", errs)
	}
	if errs := registry.StopAll(); len(errs) != 0 {
		t.Fatalf("StopAll() errors = %v, want none", errs)
	}

	want := []string{"start:first", "start:second", "stop:second", "stop:first"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
	if errs := registry.StopAll(); len(errs) != 0 {
		t.Fatalf("second StopAll() errors = %v", errs)
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("second StopAll() repeated stops: %v", order)
	}
}

func TestRegistryRejectsDuplicatesAndSupportsUnregister(t *testing.T) {
	registry := plugin.NewRegistry()
	p := testPlugin{name: "dup"}
	if err := registry.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(p); !errors.Is(err, plugin.ErrAlreadyRegistered) {
		t.Fatalf("Register() error = %v, want ErrAlreadyRegistered", err)
	}

	registry.Unregister("dup")
	if _, ok := registry.Get("dup"); ok {
		t.Fatalf("Get() after Unregister() = true, want false")
	}
}

func TestRegistryRollsBackStartedPluginsOnFailure(t *testing.T) {
	order := []string{}
	registry := plugin.NewRegistry()
	if err := registry.Register(testPlugin{name: "first", order: &order}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(testPlugin{
		name:      "second",
		order:     &order,
		startFail: errors.New("start failed"),
	}); err != nil {
		t.Fatal(err)
	}

	if errs := registry.StartAll(); len(errs) != 1 {
		t.Fatalf("StartAll() errors = %v, want one", errs)
	}
	want := []string{"start:first", "start:second", "stop:first"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
	if errs := registry.StopAll(); len(errs) != 0 {
		t.Fatalf("StopAll() after rollback errors = %v", errs)
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("StopAll() repeated rollback stops: %v", order)
	}
}

func TestRegistryRetriesFailedRollbackStop(t *testing.T) {
	first := &retryStopPlugin{name: "first"}
	registry := plugin.NewRegistry()
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(testPlugin{
		name:      "second",
		startFail: errors.New("start failed"),
	}); err != nil {
		t.Fatal(err)
	}
	if errors := registry.StartAll(); len(errors) != 2 {
		t.Fatalf("StartAll() errors = %v, want start and stop errors", errors)
	}
	if errors := registry.StopAll(); len(errors) != 0 {
		t.Fatalf("StopAll() retry errors = %v", errors)
	}
	if first.stopCalls != 2 {
		t.Fatalf("Stop() calls = %d, want 2", first.stopCalls)
	}
}

func TestDiscoverFromValuesRegistersPlugins(t *testing.T) {
	registry := plugin.NewRegistry()
	count := registry.DiscoverFromValues(testPlugin{name: "one"}, nil, testPlugin{name: "two"})
	if count != 2 {
		t.Fatalf("DiscoverFromValues() = %d, want %d", count, 2)
	}

	got := registry.Names()
	want := []string{"one", "two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestFactoryRegistryCreatesPlugins(t *testing.T) {
	factories := plugin.NewFactoryRegistry()
	if err := factories.Register("sample", func() plugin.Plugin { return testPlugin{name: "sample"} }); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := factories.Register("sample", func() plugin.Plugin { return testPlugin{name: "sample"} }); !errors.Is(err, plugin.ErrFactoryExists) {
		t.Fatalf("Register() duplicate error = %v, want ErrFactoryExists", err)
	}

	created, err := factories.Create("sample")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name() != "sample" {
		t.Fatalf("Create() plugin name = %q, want %q", created.Name(), "sample")
	}
	if _, err := factories.Create("missing"); !errors.Is(err, plugin.ErrFactoryNotFound) {
		t.Fatalf("Create() missing error = %v, want ErrFactoryNotFound", err)
	}
}
