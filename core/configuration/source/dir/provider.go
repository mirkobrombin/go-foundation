// Package dir implements configuration.Provider for directories of JSON files.
//
// Where source/file loads a single JSON document, dir loads every matching
// file in a directory and nests each document under the file name (without
// extension). This supports the "one configuration file per entity" model
// (one file per tenant, environment, instance) that a single document cannot
// express.
//
// Example:
//
//	p := dir.New("tenants", "*.json")
//	// tenants/acme.json -> keys "acme:quota", "acme:region", ...
package dir

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxFileSize = 4 << 20

// Provider implements configuration.Provider for a directory of JSON files.
type Provider struct {
	// Dir is the directory to scan.
	Dir string
	// Pattern is a glob matched against file names (e.g. "*.json").
	// Matching follows filepath.Match semantics.
	Pattern string
	// Exclude lists file names to skip (e.g. a global config living in the
	// same directory). Exact match on the base name.
	Exclude []string
}

// New creates a new directory provider. pattern is a glob like "*.json".
func New(dir, pattern string, exclude ...string) *Provider {
	return &Provider{Dir: dir, Pattern: pattern, Exclude: exclude}
}

// Name returns "dir".
func (p *Provider) Name() string {
	return "dir"
}

// Load reads every matching file in the directory and returns a flat
// key-value map. Each file's content is flattened with colon-separated keys
// prefixed by the file base name without extension:
// "tenants/acme.json" -> "acme:quota".
//
// Files are processed in sorted name order so the result is deterministic.
func (p *Provider) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		return nil, err
	}

	excluded := make(map[string]bool, len(p.Exclude))
	for _, e := range p.Exclude {
		excluded[e] = true
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || excluded[entry.Name()] {
			continue
		}
		ok, err := filepath.Match(p.Pattern, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("configuration: invalid dir pattern %q: %w", p.Pattern, err)
		}
		if ok {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	flat := make(map[string]any)
	for _, name := range names {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		data, err := readLimited(filepath.Join(p.Dir, name))
		if err != nil {
			return nil, fmt.Errorf("configuration: %s: %w", name, err)
		}

		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("configuration: %s: %w", name, err)
		}

		prefix := strings.TrimSuffix(name, filepath.Ext(name))
		flatten(prefix, parsed, flat)
	}
	return flat, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, errors.New("file exceeds 4 MiB limit")
	}
	return data, nil
}

// flatten mirrors source/file: nested objects become colon-separated keys.
func flatten(prefix string, src map[string]any, dst map[string]any) {
	for k, v := range src {
		key := prefix + ":" + k
		if nested, ok := v.(map[string]any); ok {
			flatten(key, nested, dst)
		} else {
			dst[key] = v
		}
	}
}
