package file

import (
	"context"
	"encoding/json"
	"os"
)

// Provider implements configuration.Provider for JSON files.
//
// Example:
//
//	p := file.New("config.json")
//	data, err := p.Load(ctx)
type Provider struct {
	// Path is the location of the JSON file to load.
	Path string
}

// New creates a new JSON file provider.
func New(path string) *Provider {
	return &Provider{Path: path}
}

// Name returns "file".
func (p *Provider) Name() string {
	return "file"
}

// Load reads and parses a JSON configuration file, returning a flat key-value map.
// Nested JSON objects are flattened with colon-separated keys (e.g. "db:host").
func (p *Provider) Load(ctx context.Context) (map[string]any, error) {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return nil, err
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	flat := make(map[string]any)
	flatten("", parsed, flat)
	return flat, nil
}

func flatten(prefix string, src map[string]any, dst map[string]any) {
	for k, v := range src {
		key := k
		if prefix != "" {
			key = prefix + ":" + k
		}
		if nested, ok := v.(map[string]any); ok {
			flatten(key, nested, dst)
		} else {
			dst[key] = v
		}
	}
}
