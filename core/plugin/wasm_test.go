package plugin

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

//go:embed testdata/plugin.wasm
var testWasmPlugin []byte

func testCapability(_ context.Context, operation string, input []byte) ([]byte, error) {
	if operation != "invoke" {
		return nil, errors.New("unexpected operation")
	}
	return append([]byte("host:"), input...), nil
}

func loadTestWasm(t *testing.T, options ...WasmOption) *WasmPlugin {
	t.Helper()
	options = append(options, WithWasmCapability("test.echo", CapabilityFunc(testCapability)))
	loaded, err := LoadWasm(context.Background(), testWasmPlugin, options...)
	if err != nil {
		t.Fatalf("LoadWasm() error = %v", err)
	}
	t.Cleanup(func() {
		if err := loaded.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return loaded
}

func TestLoadWasmReadsMetadataAndRequiresCapabilities(t *testing.T) {
	if _, err := LoadWasm(context.Background(), testWasmPlugin); !errors.Is(err, ErrWasmCapabilityDenied) {
		t.Fatalf("LoadWasm() error = %v, want ErrWasmCapabilityDenied", err)
	}

	loaded := loadTestWasm(t)
	metadata := loaded.Metadata()
	if metadata.Name != "fixture" || metadata.Version != "1.0.0" {
		t.Fatalf("Metadata() = %#v", metadata)
	}
	if !reflect.DeepEqual(metadata.Methods, []string{"echo", "capability", "fail", "hang"}) {
		t.Fatalf("Metadata().Methods = %v", metadata.Methods)
	}
	if metadata.Properties["language"] != "c" {
		t.Fatalf("Metadata().Properties = %v", metadata.Properties)
	}
	metadata.Methods[0] = "changed"
	metadata.Capabilities[0] = "changed"
	metadata.Properties["language"] = "changed"
	stable := loaded.Metadata()
	if stable.Methods[0] != "echo" || stable.Capabilities[0] != "test.echo" || stable.Properties["language"] != "c" {
		t.Fatalf("Metadata() returned mutable state: %#v", stable)
	}
}

func TestDecodeWasmMetadataRejectsTrailingData(t *testing.T) {
	_, err := decodeWasmMetadata([]byte(`{"name":"fixture","version":"1.0.0"}{}`))
	if !errors.Is(err, ErrWasmInvalidModule) {
		t.Fatalf("decodeWasmMetadata() error = %v, want ErrWasmInvalidModule", err)
	}
}

func TestLoadWasmRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		option WasmOption
	}{
		{name: "memory", option: WithWasmMemoryLimit(1)},
		{name: "payload", option: WithWasmPayloadLimit(0)},
		{name: "capability name", option: WithWasmCapability("Invalid", CapabilityFunc(testCapability))},
		{name: "capability handler", option: WithWasmCapability("test.echo", nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadWasm(context.Background(), testWasmPlugin, test.option)
			if !errors.Is(err, ErrWasmInvalidModule) {
				t.Fatalf("LoadWasm() error = %v, want ErrWasmInvalidModule", err)
			}
		})
	}
}

func TestWasmPluginLifecycleAndCalls(t *testing.T) {
	loaded := loadTestWasm(t)
	if _, err := loaded.Call(context.Background(), "echo", []byte("before")); !errors.Is(err, ErrWasmNotStarted) {
		t.Fatalf("Call() before Start() error = %v, want ErrWasmNotStarted", err)
	}
	if err := loaded.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := loaded.Start(); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}

	response, err := loaded.Call(context.Background(), "echo", []byte(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("Call(echo) error = %v", err)
	}
	if string(response) != `{"hello":"world"}` {
		t.Fatalf("Call(echo) = %q", response)
	}

	var decoded map[string]string
	if err := loaded.CallJSON(context.Background(), "echo", map[string]string{"source": "json"}, &decoded); err != nil {
		t.Fatalf("CallJSON() error = %v", err)
	}
	if decoded["source"] != "json" {
		t.Fatalf("CallJSON() = %v", decoded)
	}

	response, err = loaded.Call(context.Background(), "capability", []byte("request"))
	if err != nil {
		t.Fatalf("Call(capability) error = %v", err)
	}
	if string(response) != "host:request" {
		t.Fatalf("Call(capability) = %q", response)
	}

	if _, err := loaded.Call(context.Background(), "missing", nil); !errors.Is(err, ErrWasmCallFailed) {
		t.Fatalf("Call(missing) error = %v, want ErrWasmCallFailed", err)
	}
	if _, err := loaded.Call(context.Background(), "fail", nil); !errors.Is(err, ErrWasmCallFailed) {
		t.Fatalf("Call(fail) error = %v, want ErrWasmCallFailed", err)
	}
	if err := loaded.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := loaded.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func TestWasmPluginSerializesConcurrentCalls(t *testing.T) {
	loaded := loadTestWasm(t)
	if err := loaded.Start(); err != nil {
		t.Fatal(err)
	}

	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			response, err := loaded.Call(context.Background(), "capability", []byte("parallel"))
			if err != nil {
				t.Errorf("Call() error = %v", err)
				return
			}
			if string(response) != "host:parallel" {
				t.Errorf("Call() = %q", response)
			}
		}()
	}
	group.Wait()
}

func TestWasmPluginClosesAfterDeadline(t *testing.T) {
	loaded := loadTestWasm(t, WithWasmCallTimeout(20*time.Millisecond))
	if err := loaded.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Call(context.Background(), "hang", nil); !errors.Is(err, ErrWasmClosed) {
		t.Fatalf("Call(hang) error = %v, want ErrWasmClosed", err)
	}
	if !loaded.Closed() {
		t.Fatal("Closed() = false after deadline")
	}
}

func TestDiscoverWasmAndRegistryCloseAll(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.wasm")
	if err := os.WriteFile(path, testWasmPlugin, 0o600); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	count, err := registry.RegisterWasmDirectory(
		context.Background(),
		directory,
		WithWasmCapability("test.echo", CapabilityFunc(testCapability)),
	)
	if err != nil {
		t.Fatalf("RegisterWasmDirectory() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("RegisterWasmDirectory() = %d, want 1", count)
	}
	if errs := registry.StartAll(); len(errs) != 0 {
		t.Fatalf("StartAll() errors = %v", errs)
	}
	registered, _ := registry.Get("fixture")
	loaded, _ := registered.(*WasmPlugin)
	if errs := registry.CloseAll(context.Background()); len(errs) != 0 {
		t.Fatalf("CloseAll() errors = %v", errs)
	}
	if !loaded.Closed() {
		t.Fatal("registered plugin remained open")
	}
}
