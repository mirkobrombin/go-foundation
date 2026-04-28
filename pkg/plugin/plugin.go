// Package plugin provides a focused plugin registry and supporting helpers.
package plugin

import "errors"

var (
	ErrAlreadyRegistered = errors.New("plugin: already registered")
	ErrFactoryExists     = errors.New("plugin: factory already exists")
	ErrFactoryNotFound   = errors.New("plugin: factory not found")
)

// Plugin defines the lifecycle interface for all plugins.
type Plugin interface {
	Name() string
	Start() error
	Stop() error
}

// Factory is a function that creates a new plugin instance.
type Factory func() Plugin