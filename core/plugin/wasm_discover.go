package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverWasm loads every .wasm plugin in a directory in filename order.
func DiscoverWasm(ctx context.Context, directory string, options ...WasmOption) ([]*WasmPlugin, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("plugin: read WebAssembly directory: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".wasm") {
			continue
		}
		paths = append(paths, filepath.Join(directory, entry.Name()))
	}
	sort.Strings(paths)
	loaded := make([]*WasmPlugin, 0, len(paths))
	for _, path := range paths {
		module, err := LoadWasmFile(ctx, path, options...)
		if err != nil {
			closeWasm(ctx, loaded)
			return nil, err
		}
		loaded = append(loaded, module)
	}
	return loaded, nil
}

// RegisterWasmDirectory loads and registers every .wasm plugin in a directory.
// The registry owns successfully registered modules and should close them with CloseAll.
func (registry *Registry) RegisterWasmDirectory(ctx context.Context, directory string, options ...WasmOption) (int, error) {
	loaded, err := DiscoverWasm(ctx, directory, options...)
	if err != nil {
		return 0, err
	}
	registered := 0
	for index, module := range loaded {
		if err := registry.Register(module); err != nil {
			_ = module.Close(ctx)
			closeWasm(ctx, loaded[index+1:])
			return registered, err
		}
		registered++
	}
	return registered, nil
}

func closeWasm(ctx context.Context, plugins []*WasmPlugin) {
	for _, plugin := range plugins {
		_ = plugin.Close(ctx)
	}
}
