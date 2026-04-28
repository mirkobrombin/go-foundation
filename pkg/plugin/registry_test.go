package plugin_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/plugin"
)

type testPlugin struct {
	name  string
	order *[]string
	fail  error
}

func (p testPlugin) Name() string { return p.name }

func (p testPlugin) Start() error {
	if p.order != nil {
		*p.order = append(*p.order, "start:"+p.name)
	}
	return p.fail
}

func (p testPlugin) Stop() error {
	if p.order != nil {
		*p.order = append(*p.order, "stop:"+p.name)
	}
	return p.fail
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
