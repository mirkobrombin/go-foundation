// Package plugin provides a focused plugin registry and supporting helpers.
package plugin

import "errors"

var (
	// ErrAlreadyRegistered indicates a plugin with the same name is already registered.
	ErrAlreadyRegistered = errors.New("plugin: already registered")
	// ErrFactoryExists indicates a factory for the given name already exists.
	ErrFactoryExists = errors.New("plugin: factory already exists")
	// ErrFactoryNotFound indicates no factory exists for the requested name.
	ErrFactoryNotFound = errors.New("plugin: factory not found")
)

// Plugin defines the lifecycle interface for all plugins.
type Plugin interface {
	Name() string
	Start() error
	Stop() error
}

// Factory is a function that creates a new plugin instance.
type Factory func() Plugin