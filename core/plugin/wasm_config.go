package plugin

import (
	"fmt"
	"io"
	"io/fs"
	"math"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
)

const (
	defaultWasmMemoryLimit  = 64 << 20
	defaultWasmPayloadLimit = 8 << 20
	defaultWasmCallTimeout  = 5 * time.Second
	wasmPageSize            = 64 << 10
)

type wasmFilesystem struct {
	fs        fs.FS
	guestPath string
}

type wasmConfig struct {
	capabilities map[string]Capability
	memoryLimit  uint64
	payloadLimit uint32
	callTimeout  time.Duration
	wasi         bool
	wasiArgs     []string
	wasiEnv      map[string]string
	wasiFS       []wasmFilesystem
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	random       io.Reader
	systemClock  bool
	cache        wazero.CompilationCache
}

func defaultWasmConfig() wasmConfig {
	return wasmConfig{
		capabilities: make(map[string]Capability),
		memoryLimit:  defaultWasmMemoryLimit,
		payloadLimit: defaultWasmPayloadLimit,
		callTimeout:  defaultWasmCallTimeout,
		wasiEnv:      make(map[string]string),
	}
}

// WasmOption configures a WebAssembly plugin runtime before the module loads.
type WasmOption func(*wasmConfig)

// WithWasmCapability grants one named host capability to a module.
func WithWasmCapability(name string, capability Capability) WasmOption {
	return func(config *wasmConfig) {
		config.capabilities[name] = capability
	}
}

// WithWasmMemoryLimit sets the maximum linear memory available to a module.
func WithWasmMemoryLimit(bytes uint64) WasmOption {
	return func(config *wasmConfig) {
		config.memoryLimit = bytes
	}
}

// WithWasmPayloadLimit sets the maximum request, response, and error size.
func WithWasmPayloadLimit(bytes uint32) WasmOption {
	return func(config *wasmConfig) {
		config.payloadLimit = bytes
	}
}

// WithWasmCallTimeout sets the default lifecycle and method call deadline.
// A non-positive duration leaves deadlines to the supplied context.
func WithWasmCallTimeout(timeout time.Duration) WasmOption {
	return func(config *wasmConfig) {
		config.callTimeout = timeout
	}
}

// WithWasmCompilationCache shares compiled WebAssembly code across runtimes.
// The caller owns the cache and must close it after every plugin using it closes.
func WithWasmCompilationCache(cache wazero.CompilationCache) WasmOption {
	return func(config *wasmConfig) {
		config.cache = cache
	}
}

// WithWasmWASI enables WASI Preview 1 with no inherited host resources.
func WithWasmWASI() WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
	}
}

// WithWasmWASIArgs exposes an explicit argument vector through WASI.
func WithWasmWASIArgs(args ...string) WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.wasiArgs = append([]string(nil), args...)
	}
}

// WithWasmWASIEnv exposes one environment variable through WASI.
func WithWasmWASIEnv(key, value string) WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.wasiEnv[key] = value
	}
}

// WithWasmWASIFS mounts an explicit fs.FS at a guest path.
// The caller is responsible for the isolation guarantees of the supplied filesystem.
func WithWasmWASIFS(filesystem fs.FS, guestPath string) WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.wasiFS = append(config.wasiFS, wasmFilesystem{fs: filesystem, guestPath: guestPath})
	}
}

// WithWasmWASIStdin exposes an explicit input stream through WASI.
func WithWasmWASIStdin(reader io.Reader) WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.stdin = reader
	}
}

// WithWasmWASIStdout exposes an explicit output stream through WASI.
func WithWasmWASIStdout(writer io.Writer) WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.stdout = writer
	}
}

// WithWasmWASIStderr exposes an explicit error stream through WASI.
func WithWasmWASIStderr(writer io.Writer) WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.stderr = writer
	}
}

// WithWasmWASIRandom exposes an explicit random source through WASI.
func WithWasmWASIRandom(reader io.Reader) WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.random = reader
	}
}

// WithWasmWASISystemClock exposes the host wall clock, monotonic clock, and sleep.
func WithWasmWASISystemClock() WasmOption {
	return func(config *wasmConfig) {
		config.wasi = true
		config.systemClock = true
	}
}

func (config wasmConfig) validate() error {
	if config.memoryLimit < wasmPageSize || config.memoryLimit > uint64(math.MaxUint32)+1 {
		return fmt.Errorf("%w: memory limit must be between 64 KiB and 4 GiB", ErrWasmInvalidModule)
	}
	if config.payloadLimit == 0 || config.payloadLimit > math.MaxInt32 {
		return fmt.Errorf("%w: payload limit must be between 1 and %d bytes", ErrWasmInvalidModule, math.MaxInt32)
	}
	for name, capability := range config.capabilities {
		if err := validateWasmName(name); err != nil {
			return fmt.Errorf("%w: capability %q: %v", ErrWasmInvalidModule, name, err)
		}
		if capability == nil {
			return fmt.Errorf("%w: capability %q has no handler", ErrWasmInvalidModule, name)
		}
		if function, ok := capability.(CapabilityFunc); ok && function == nil {
			return fmt.Errorf("%w: capability %q has no handler", ErrWasmInvalidModule, name)
		}
	}
	for key := range config.wasiEnv {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00") {
			return fmt.Errorf("%w: invalid WASI environment key %q", ErrWasmInvalidModule, key)
		}
	}
	for _, mount := range config.wasiFS {
		if mount.fs == nil || strings.TrimSpace(mount.guestPath) == "" {
			return fmt.Errorf("%w: invalid WASI filesystem mount", ErrWasmInvalidModule)
		}
	}
	return nil
}

func (config wasmConfig) memoryPages() uint32 {
	return uint32((config.memoryLimit + wasmPageSize - 1) / wasmPageSize)
}
