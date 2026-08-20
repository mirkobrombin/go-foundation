// Package plugin provides plugin lifecycle, discovery, and isolated execution.
package plugin

import "errors"

var (
	// ErrAlreadyRegistered indicates a plugin with the same name is already registered.
	ErrAlreadyRegistered = errors.New("plugin: already registered")
	// ErrFactoryExists indicates a factory for the given name already exists.
	ErrFactoryExists = errors.New("plugin: factory already exists")
	// ErrFactoryNotFound indicates no factory exists for the requested name.
	ErrFactoryNotFound = errors.New("plugin: factory not found")
	// ErrWasmABIMismatch indicates a module targets an unsupported Foundation plugin ABI.
	ErrWasmABIMismatch = errors.New("plugin: unsupported WebAssembly ABI")
	// ErrWasmInvalidModule indicates a module does not satisfy the Foundation plugin contract.
	ErrWasmInvalidModule = errors.New("plugin: invalid WebAssembly module")
	// ErrWasmCapabilityDenied indicates a module requested a host capability it was not granted.
	ErrWasmCapabilityDenied = errors.New("plugin: WebAssembly capability denied")
	// ErrWasmNotStarted indicates a call was attempted before the plugin started.
	ErrWasmNotStarted = errors.New("plugin: WebAssembly plugin not started")
	// ErrWasmClosed indicates a call was attempted after the plugin runtime closed.
	ErrWasmClosed = errors.New("plugin: WebAssembly plugin closed")
	// ErrWasmCallFailed indicates a guest method or lifecycle function returned an error.
	ErrWasmCallFailed = errors.New("plugin: WebAssembly call failed")
)

// Plugin defines the lifecycle interface for all plugins.
type Plugin interface {
	Name() string
	Start() error
	Stop() error
}

// Factory is a function that creates a new plugin instance.
type Factory func() Plugin
