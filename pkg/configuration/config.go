// Package configuration provides a flexible, multi-source configuration system
// inspired by .NET's IConfiguration pattern. Configuration values can be loaded
// from environment variables, JSON files, command-line flags, and custom sources,
// then merged with priority ordering.
package configuration

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// Provider is the interface that configuration sources must implement.
//
// Example:
//
//	type MyProvider struct{}
//	func (p *MyProvider) Name() string { return "my" }
//	func (p *MyProvider) Load(ctx) (map[string]any, error) { return map[string]any{"key": "val"}, nil }
type Provider interface {
	// Name returns a human-readable identifier for this source.
	Name() string

	// Load retrieves configuration key-value pairs.
	Load(ctx context.Context) (map[string]any, error)
}

// Configuration holds merged configuration data from multiple providers.
//
// Example:
//
//	cfg := configuration.NewBuilder().
//	    AddEnv("APP_").
//	    AddJSONFile("config.json").
//	    Build()
//	val := cfg.Get("db:host")
type Configuration struct {
	mu   sync.RWMutex
	data map[string]any
}

// Get returns the value for the given key. The key is case-insensitive.
// Nested keys can be accessed with colon syntax, e.g. "db:host".
//
// Returns:
//
// The value and true if found, otherwise nil and false.
func (c *Configuration) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[strings.ToLower(key)]
	return v, ok
}

// GetString returns the string value for the given key.
//
// Returns:
//
// The value and true if the key exists and is a string.
func (c *Configuration) GetString(key string) (string, bool) {
	v, ok := c.Get(key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// GetInt returns the int value for the given key.
//
// Returns:
//
// The value and true if the key exists and can be converted to int.
func (c *Configuration) GetInt(key string) (int, bool) {
	v, ok := c.Get(key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	}
	return 0, false
}

// GetBool returns the bool value for the given key.
//
// Returns:
//
// The value and true if the key exists and can be converted to bool.
func (c *Configuration) GetBool(key string) (bool, bool) {
	v, ok := c.Get(key)
	if !ok {
		return false, false
	}
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		return strings.ToLower(b) == "true" || b == "1", true
	}
	return false, false
}

// GetSection returns a new Configuration containing only keys with the given prefix.
func (c *Configuration) GetSection(prefix string) *Configuration {
	prefix = strings.ToLower(prefix) + ":"
	c.mu.RLock()
	defer c.mu.RUnlock()

	section := New()
	for k, v := range c.data {
		if strings.HasPrefix(k, prefix) {
			section.data[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return section
}

// Bind populates a struct from the configuration data. Fields are matched using
// the `conf` tag. If no tag is present, the field name is used as key.
//
// Example:
//
//	type Config struct {
//	    Host string `conf:"db:host"`
//	    Port int    `conf:"db:port" default:"5432"`
//	}
//	var cfg Config
//	err := configuration.Bind(&cfg)
func (c *Configuration) Bind(target any) error {
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("configuration: target must be a pointer to a struct")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	elem := val.Elem()
	confParser := tags.NewParser("conf", tags.WithPairDelimiter(","))
	fields := confParser.ParseStruct(target)

	for _, meta := range fields {
		fieldVal := elem.Field(meta.Index)
		if !fieldVal.CanSet() {
			continue
		}

		key := meta.RawTag
		if key == "" {
			key = strings.ToLower(meta.Name)
		}

		var rawVal any
		found := false

		if v, ok := c.data[strings.ToLower(key)]; ok {
			rawVal = v
			found = true
		}

		if !found {
			if envKey := meta.Get("env"); envKey != "" {
				if v, ok := c.data[strings.ToLower(envKey)]; ok {
					rawVal = v
					found = true
				}
			}
		}

		if !found {
			if def := meta.Get("default"); def != "" {
				rawVal = def
				found = true
			}
		}

		if !found {
			continue
		}

		if err := setField(fieldVal, fmt.Sprintf("%v", rawVal)); err != nil {
			return fmt.Errorf("configuration: field %s: %w", meta.Name, err)
		}
	}

	return nil
}

// New creates an empty Configuration.
func New() *Configuration {
	return &Configuration{
		data: make(map[string]any),
	}
}

// Builder provides a fluent API for assembling a Configuration from multiple
// sources with priority ordering (last added wins).
//
// Example:
//
//	cfg := configuration.NewBuilder().
//	    AddEnv("APP_").
//	    AddJSONFile("config.json").
//	    Build()
type Builder struct {
	providers []Provider
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Add registers a configuration provider. Providers added later take priority
// over earlier ones for overlapping keys.
func (b *Builder) Add(p Provider) *Builder {
	b.providers = append(b.providers, p)
	return b
}

// Build merges all registered providers into a single Configuration.
// Providers are queried in order; later providers override earlier ones.
func (b *Builder) Build(ctx context.Context) (*Configuration, error) {
	cfg := New()
	for _, p := range b.providers {
		data, err := p.Load(ctx)
		if err != nil {
			return nil, fmt.Errorf("configuration: source %q failed: %w", p.Name(), err)
		}
		cfg.mu.Lock()
		for k, v := range data {
			cfg.data[strings.ToLower(k)] = v
		}
		cfg.mu.Unlock()
	}
	return cfg, nil
}

func setField(field reflect.Value, valStr string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(valStr)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(valStr, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(valStr)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return err
		}
		field.SetFloat(n)
	default:
		return fmt.Errorf("unsupported type %s", field.Kind())
	}
	return nil
}
