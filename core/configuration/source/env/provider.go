package env

import (
	"context"
	"os"
	"strings"
)

// Provider implements configuration.Provider for environment variables.
//
// Example:
//
//	p := &env.Provider{Prefix: "APP_"}
//	data, err := p.Load(ctx)
type Provider struct {
	// Prefix is an optional prefix to filter environment variables.
	Prefix string
}

// New creates a new environment variable provider with an optional prefix.
func New(prefix string) *Provider {
	return &Provider{Prefix: prefix}
}

// Name returns "env".
func (p *Provider) Name() string {
	return "env"
}

// Load reads all environment variables, filtering by prefix if set.
func (p *Provider) Load(ctx context.Context) (map[string]any, error) {
	data := make(map[string]any)
	env := os.Environ()

	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}

		if p.Prefix != "" {
			if strings.HasPrefix(k, p.Prefix) {
				key := strings.TrimPrefix(k, p.Prefix)
				if strings.HasSuffix(p.Prefix, "_") {
					// Prefix include underscore
				} else if strings.HasPrefix(key, "_") {
					key = strings.TrimPrefix(key, "_")
				}
				data[strings.ToLower(key)] = v
			}
		} else {
			data[strings.ToLower(k)] = v
		}
	}
	return data, nil
}
