package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	// WasmABIName identifies the Foundation WebAssembly plugin contract.
	WasmABIName = "foundation:plugin"
	// WasmABIMajor is the incompatible ABI generation supported by this host.
	WasmABIMajor uint32 = 1
	// WasmABIMinor is the newest backwards-compatible ABI revision supported by this host.
	WasmABIMinor uint32 = 0

	wasmHostModule       = "foundation"
	wasmErrorResult      = ^uint64(0)
	maxWasmMetadataSize  = 64 << 10
	maxWasmNameSize      = 128
	maxWasmDescription   = 4 << 10
	wasmExportVersion    = "foundation_abi_version"
	wasmExportAllocate   = "foundation_alloc"
	wasmExportFree       = "foundation_free"
	wasmExportMetadata   = "foundation_metadata"
	wasmExportStart      = "foundation_start"
	wasmExportStop       = "foundation_stop"
	wasmExportCall       = "foundation_call"
	wasmExportLastError  = "foundation_last_error"
	wasmImportHostCall   = "host_call"
	wasmImportResultLen  = "host_response_len"
	wasmImportResultRead = "host_response_read"
	wasmImportErrorLen   = "host_error_len"
	wasmImportErrorRead  = "host_error_read"
)

// WasmHostStatus is returned to a guest after a host capability call.
type WasmHostStatus uint32

const (
	// WasmHostOK indicates the capability call completed.
	WasmHostOK WasmHostStatus = iota
	// WasmHostDenied indicates the capability was not declared or granted.
	WasmHostDenied
	// WasmHostInvalidRequest indicates a name, operation, or memory range was invalid.
	WasmHostInvalidRequest
	// WasmHostHandlerError indicates the capability handler returned an error.
	WasmHostHandlerError
	// WasmHostPayloadTooLarge indicates a request or response exceeded the configured limit.
	WasmHostPayloadTooLarge
)

// Capability handles one operation requested by a WebAssembly plugin.
type Capability interface {
	Call(context.Context, string, []byte) ([]byte, error)
}

// CapabilityFunc adapts a function into a Capability.
type CapabilityFunc func(context.Context, string, []byte) ([]byte, error)

// Call invokes the capability function.
func (function CapabilityFunc) Call(ctx context.Context, operation string, input []byte) ([]byte, error) {
	if function == nil {
		return nil, ErrWasmCapabilityDenied
	}
	return function(ctx, operation, input)
}

// WasmMetadata describes a module before its lifecycle starts.
type WasmMetadata struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description,omitempty"`
	Methods      []string          `json:"methods,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Properties   map[string]string `json:"properties,omitempty"`
}

// WasmPlugin is an in-process plugin isolated by a WebAssembly runtime.
type WasmPlugin struct {
	mu           sync.Mutex
	runtime      wazero.Runtime
	compiled     wazero.CompiledModule
	module       api.Module
	config       wasmConfig
	metadata     WasmMetadata
	methods      map[string]struct{}
	declared     map[string]struct{}
	hostResponse []byte
	hostError    []byte
	started      bool
	closed       bool
}

// LoadWasmFile loads and validates a WebAssembly plugin from disk.
func LoadWasmFile(ctx context.Context, path string, options ...WasmOption) (*WasmPlugin, error) {
	binary, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plugin: read WebAssembly module: %w", err)
	}
	loaded, err := LoadWasm(ctx, binary, options...)
	if err != nil {
		return nil, fmt.Errorf("plugin: load %s: %w", path, err)
	}
	return loaded, nil
}

// LoadWasm loads and validates a WebAssembly plugin binary.
func LoadWasm(ctx context.Context, binary []byte, options ...WasmOption) (*WasmPlugin, error) {
	config := defaultWasmConfig()
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("%w: empty binary", ErrWasmInvalidModule)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runtimeConfig := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(config.memoryPages()).
		WithCloseOnContextDone(true)
	if config.cache != nil {
		runtimeConfig = runtimeConfig.WithCompilationCache(config.cache)
	}

	loaded := &WasmPlugin{
		config:   config,
		methods:  make(map[string]struct{}),
		declared: make(map[string]struct{}),
	}
	loaded.runtime = wazero.NewRuntimeWithConfig(ctx, runtimeConfig)
	if err := loaded.instantiateHost(ctx); err != nil {
		_ = loaded.runtime.Close(ctx)
		return nil, err
	}
	if config.wasi {
		if _, err := wasi_snapshot_preview1.Instantiate(ctx, loaded.runtime); err != nil {
			_ = loaded.runtime.Close(ctx)
			return nil, fmt.Errorf("%w: instantiate WASI: %v", ErrWasmInvalidModule, err)
		}
	}

	compiled, err := loaded.runtime.CompileModule(ctx, binary)
	if err != nil {
		_ = loaded.runtime.Close(ctx)
		return nil, fmt.Errorf("%w: compile: %v", ErrWasmInvalidModule, err)
	}
	loaded.compiled = compiled
	if err := loaded.validateContract(); err != nil {
		_ = loaded.runtime.Close(ctx)
		return nil, err
	}

	module, err := loaded.runtime.InstantiateModule(ctx, compiled, loaded.moduleConfig())
	if err != nil {
		_ = loaded.runtime.Close(ctx)
		return nil, fmt.Errorf("%w: instantiate: %v", ErrWasmInvalidModule, err)
	}
	loaded.module = module
	if err := loaded.loadMetadata(ctx); err != nil {
		_ = loaded.runtime.Close(ctx)
		return nil, err
	}
	return loaded, nil
}

// Name returns the stable plugin name declared by the guest.
func (plugin *WasmPlugin) Name() string {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	return plugin.metadata.Name
}

// Metadata returns a copy of the guest metadata.
func (plugin *WasmPlugin) Metadata() WasmMetadata {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	metadata := plugin.metadata
	metadata.Methods = slices.Clone(metadata.Methods)
	metadata.Capabilities = slices.Clone(metadata.Capabilities)
	metadata.Properties = cloneStrings(metadata.Properties)
	return metadata
}

// Start starts the guest with the configured default deadline.
func (plugin *WasmPlugin) Start() error {
	return plugin.StartContext(context.Background())
}

// StartContext starts the guest lifecycle.
func (plugin *WasmPlugin) StartContext(ctx context.Context) error {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if err := plugin.checkOpen(); err != nil {
		return err
	}
	if plugin.started {
		return nil
	}
	if err := plugin.callStatus(ctx, wasmExportStart); err != nil {
		return fmt.Errorf("%w: start: %v", ErrWasmCallFailed, err)
	}
	plugin.started = true
	return nil
}

// Stop stops the guest with the configured default deadline.
func (plugin *WasmPlugin) Stop() error {
	return plugin.StopContext(context.Background())
}

// StopContext stops the guest lifecycle.
func (plugin *WasmPlugin) StopContext(ctx context.Context) error {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if err := plugin.checkOpen(); err != nil {
		return err
	}
	if !plugin.started {
		return nil
	}
	if err := plugin.callStatus(ctx, wasmExportStop); err != nil {
		return fmt.Errorf("%w: stop: %v", ErrWasmCallFailed, err)
	}
	plugin.started = false
	return nil
}

// Call invokes a declared guest method with an opaque byte payload.
func (plugin *WasmPlugin) Call(ctx context.Context, method string, input []byte) ([]byte, error) {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if err := plugin.checkOpen(); err != nil {
		return nil, err
	}
	if !plugin.started {
		return nil, ErrWasmNotStarted
	}
	if _, ok := plugin.methods[method]; !ok {
		return nil, fmt.Errorf("%w: method %q is not declared", ErrWasmCallFailed, method)
	}
	if len(input) > int(plugin.config.payloadLimit) {
		return nil, fmt.Errorf("%w: request exceeds %d bytes", ErrWasmCallFailed, plugin.config.payloadLimit)
	}

	methodPointer, err := plugin.writeGuest(ctx, []byte(method))
	if err != nil {
		return nil, err
	}
	defer plugin.freeGuest(methodPointer, uint32(len(method)))
	inputPointer, err := plugin.writeGuest(ctx, input)
	if err != nil {
		return nil, err
	}
	defer plugin.freeGuest(inputPointer, uint32(len(input)))

	callCtx, cancel := plugin.context(ctx)
	defer cancel()
	result, err := plugin.module.ExportedFunction(wasmExportCall).Call(
		callCtx,
		uint64(methodPointer),
		uint64(len(method)),
		uint64(inputPointer),
		uint64(len(input)),
	)
	if err != nil {
		return nil, plugin.runtimeError(err)
	}
	if result[0] == wasmErrorResult {
		return nil, fmt.Errorf("%w: %s", ErrWasmCallFailed, plugin.lastGuestError(callCtx))
	}
	outputPointer, outputLength := unpackWasmBuffer(result[0])
	if outputLength > plugin.config.payloadLimit {
		return nil, fmt.Errorf("%w: response exceeds %d bytes", ErrWasmCallFailed, plugin.config.payloadLimit)
	}
	output, err := plugin.readGuest(outputPointer, outputLength)
	if err != nil {
		return nil, err
	}
	plugin.freeGuest(outputPointer, outputLength)
	return output, nil
}

// CallJSON marshals input, invokes a method, and unmarshals its response.
func (plugin *WasmPlugin) CallJSON(ctx context.Context, method string, input, output any) error {
	request, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("plugin: encode WebAssembly request: %w", err)
	}
	response, err := plugin.Call(ctx, method, request)
	if err != nil {
		return err
	}
	if output == nil || len(response) == 0 {
		return nil
	}
	if err := json.Unmarshal(response, output); err != nil {
		return fmt.Errorf("plugin: decode WebAssembly response: %w", err)
	}
	return nil
}

// Close stops the plugin if needed and releases its runtime.
func (plugin *WasmPlugin) Close(ctx context.Context) error {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if plugin.closed {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var stopErr error
	if plugin.started && plugin.module != nil && !plugin.module.IsClosed() {
		if err := plugin.callStatus(ctx, wasmExportStop); err != nil {
			stopErr = fmt.Errorf("%w: stop: %v", ErrWasmCallFailed, err)
		}
	}
	plugin.started = false
	plugin.closed = true
	return errors.Join(stopErr, plugin.runtime.Close(ctx))
}

// Closed reports whether the plugin runtime can accept more calls.
func (plugin *WasmPlugin) Closed() bool {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	return plugin.closed || plugin.module == nil || plugin.module.IsClosed()
}

func (plugin *WasmPlugin) moduleConfig() wazero.ModuleConfig {
	config := wazero.NewModuleConfig().WithName("").WithStartFunctions()
	if len(plugin.config.wasiArgs) > 0 {
		config = config.WithArgs(plugin.config.wasiArgs...)
	}
	for key, value := range plugin.config.wasiEnv {
		config = config.WithEnv(key, value)
	}
	if len(plugin.config.wasiFS) > 0 {
		filesystems := wazero.NewFSConfig()
		for _, mount := range plugin.config.wasiFS {
			filesystems = filesystems.WithFSMount(mount.fs, mount.guestPath)
		}
		config = config.WithFSConfig(filesystems)
	}
	if plugin.config.stdin != nil {
		config = config.WithStdin(plugin.config.stdin)
	}
	if plugin.config.stdout != nil {
		config = config.WithStdout(plugin.config.stdout)
	}
	if plugin.config.stderr != nil {
		config = config.WithStderr(plugin.config.stderr)
	}
	if plugin.config.random != nil {
		config = config.WithRandSource(plugin.config.random)
	}
	if plugin.config.systemClock {
		config = config.WithSysWalltime().WithSysNanotime().WithSysNanosleep()
	}
	return config
}

func (plugin *WasmPlugin) loadMetadata(ctx context.Context) error {
	callCtx, cancel := plugin.context(ctx)
	defer cancel()
	version, err := plugin.module.ExportedFunction(wasmExportVersion).Call(callCtx)
	if err != nil {
		return plugin.runtimeError(err)
	}
	major, minor := uint32(version[0]>>32), uint32(version[0])
	if major != WasmABIMajor || minor > WasmABIMinor {
		return fmt.Errorf("%w: guest %d.%d, host %d.%d", ErrWasmABIMismatch, major, minor, WasmABIMajor, WasmABIMinor)
	}

	result, err := plugin.module.ExportedFunction(wasmExportMetadata).Call(callCtx)
	if err != nil {
		return plugin.runtimeError(err)
	}
	pointer, length := unpackWasmBuffer(result[0])
	if length == 0 || length > maxWasmMetadataSize {
		return fmt.Errorf("%w: metadata must contain 1 to %d bytes", ErrWasmInvalidModule, maxWasmMetadataSize)
	}
	data, err := plugin.readGuest(pointer, length)
	if err != nil {
		return err
	}
	metadata, err := decodeWasmMetadata(data)
	if err != nil {
		return err
	}
	plugin.metadata = metadata
	if err := plugin.validateMetadata(); err != nil {
		return err
	}
	return nil
}

func decodeWasmMetadata(data []byte) (WasmMetadata, error) {
	var metadata WasmMetadata
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&metadata); err != nil {
		return WasmMetadata{}, fmt.Errorf("%w: decode metadata: %v", ErrWasmInvalidModule, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return WasmMetadata{}, fmt.Errorf("%w: metadata contains trailing data", ErrWasmInvalidModule)
	}
	return metadata, nil
}

func (plugin *WasmPlugin) validateMetadata() error {
	if err := validateWasmName(plugin.metadata.Name); err != nil {
		return fmt.Errorf("%w: plugin name: %v", ErrWasmInvalidModule, err)
	}
	if strings.TrimSpace(plugin.metadata.Version) == "" || len(plugin.metadata.Version) > maxWasmNameSize {
		return fmt.Errorf("%w: plugin version must contain 1 to %d bytes", ErrWasmInvalidModule, maxWasmNameSize)
	}
	if len(plugin.metadata.Description) > maxWasmDescription {
		return fmt.Errorf("%w: plugin description exceeds %d bytes", ErrWasmInvalidModule, maxWasmDescription)
	}
	for _, method := range plugin.metadata.Methods {
		if err := validateWasmName(method); err != nil {
			return fmt.Errorf("%w: method %q: %v", ErrWasmInvalidModule, method, err)
		}
		if _, exists := plugin.methods[method]; exists {
			return fmt.Errorf("%w: duplicate method %q", ErrWasmInvalidModule, method)
		}
		plugin.methods[method] = struct{}{}
	}
	for _, capability := range plugin.metadata.Capabilities {
		if err := validateWasmName(capability); err != nil {
			return fmt.Errorf("%w: capability %q: %v", ErrWasmInvalidModule, capability, err)
		}
		if _, exists := plugin.declared[capability]; exists {
			return fmt.Errorf("%w: duplicate capability %q", ErrWasmInvalidModule, capability)
		}
		plugin.declared[capability] = struct{}{}
		if _, granted := plugin.config.capabilities[capability]; !granted {
			return fmt.Errorf("%w: %s", ErrWasmCapabilityDenied, capability)
		}
	}
	return nil
}

func (plugin *WasmPlugin) callStatus(ctx context.Context, name string) error {
	callCtx, cancel := plugin.context(ctx)
	defer cancel()
	result, err := plugin.module.ExportedFunction(name).Call(callCtx)
	if err != nil {
		return plugin.runtimeError(err)
	}
	if result[0] != 0 {
		return errors.New(plugin.lastGuestError(callCtx))
	}
	return nil
}

func (plugin *WasmPlugin) writeGuest(ctx context.Context, data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, nil
	}
	callCtx, cancel := plugin.context(ctx)
	defer cancel()
	result, err := plugin.module.ExportedFunction(wasmExportAllocate).Call(callCtx, uint64(len(data)))
	if err != nil {
		return 0, plugin.runtimeError(err)
	}
	pointer := uint32(result[0])
	if pointer == 0 || !plugin.module.Memory().Write(pointer, data) {
		return 0, fmt.Errorf("%w: guest allocation is outside memory", ErrWasmInvalidModule)
	}
	return pointer, nil
}

func (plugin *WasmPlugin) freeGuest(pointer, length uint32) {
	if pointer == 0 || plugin.module == nil || plugin.module.IsClosed() {
		return
	}
	ctx, cancel := plugin.context(context.Background())
	defer cancel()
	_, _ = plugin.module.ExportedFunction(wasmExportFree).Call(ctx, uint64(pointer), uint64(length))
}

func (plugin *WasmPlugin) readGuest(pointer, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}
	data, ok := plugin.module.Memory().Read(pointer, length)
	if !ok {
		return nil, fmt.Errorf("%w: guest buffer is outside memory", ErrWasmInvalidModule)
	}
	return bytes.Clone(data), nil
}

func (plugin *WasmPlugin) lastGuestError(ctx context.Context) string {
	result, err := plugin.module.ExportedFunction(wasmExportLastError).Call(ctx)
	if err != nil || len(result) == 0 {
		return "guest did not provide an error"
	}
	pointer, length := unpackWasmBuffer(result[0])
	if length == 0 || length > plugin.config.payloadLimit {
		return "guest returned an invalid error"
	}
	message, err := plugin.readGuest(pointer, length)
	if err != nil {
		return "guest returned an invalid error"
	}
	return string(message)
}

func (plugin *WasmPlugin) checkOpen() error {
	if plugin.closed || plugin.module == nil || plugin.module.IsClosed() {
		return ErrWasmClosed
	}
	return nil
}

func (plugin *WasmPlugin) runtimeError(err error) error {
	if plugin.module == nil || plugin.module.IsClosed() {
		return errors.Join(ErrWasmClosed, err)
	}
	return fmt.Errorf("%w: %v", ErrWasmCallFailed, err)
}

func (plugin *WasmPlugin) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if plugin.config.callTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= plugin.config.callTimeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, plugin.config.callTimeout)
}

func unpackWasmBuffer(value uint64) (uint32, uint32) {
	return uint32(value >> 32), uint32(value)
}

func validateWasmName(name string) error {
	if len(name) == 0 || len(name) > maxWasmNameSize {
		return fmt.Errorf("must contain 1 to %d bytes", maxWasmNameSize)
	}
	for index, character := range name {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' && index > 0 || index > 0 && strings.ContainsRune("._-", character) {
			continue
		}
		return fmt.Errorf("contains invalid character %q", character)
	}
	return nil
}

func cloneStrings(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
