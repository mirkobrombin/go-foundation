package hooks

import (
	"context"
	"reflect"
	"strings"
	"sync"
)

// Discovery finds lifecycle methods on structs via reflection.
//
// Example:
//
//	d := hooks.NewDiscovery()
//	methods := d.Discover(myService, "OnStart")
type Discovery struct {
	cache sync.Map
}

// NewDiscovery creates a hook discovery instance with caching.
func NewDiscovery() *Discovery {
	return &Discovery{}
}

// MethodInfo holds information about a discovered method.
type MethodInfo struct {
	Name   string
	Suffix string
	Method reflect.Method
	Value  reflect.Value
}

// Discover finds all methods on v with the given prefix.
//
// Returns:
//
// A list of discovered matching methods.
func (d *Discovery) Discover(v any, prefix string) []MethodInfo {
	val := reflect.ValueOf(v)
	typ := val.Type()

	cacheKey := typ.String() + ":" + prefix
	if cached, ok := d.cache.Load(cacheKey); ok {
		return d.resolveFromCache(val, cached.([]MethodInfo))
	}

	var methods []MethodInfo

	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		if strings.HasPrefix(method.Name, prefix) {
			suffix := strings.TrimPrefix(method.Name, prefix)
			methods = append(methods, MethodInfo{
				Name:   method.Name,
				Suffix: suffix,
				Method: method,
			})
		}
	}

	d.cache.Store(cacheKey, methods)
	return d.resolveFromCache(val, methods)
}

func (d *Discovery) resolveFromCache(val reflect.Value, cached []MethodInfo) []MethodInfo {
	result := make([]MethodInfo, len(cached))
	for i, m := range cached {
		result[i] = MethodInfo{
			Name:   m.Name,
			Suffix: m.Suffix,
			Method: m.Method,
			Value:  val.MethodByName(m.Name),
		}
	}
	return result
}

// DiscoverAll finds methods matching any of the given prefixes.
func (d *Discovery) DiscoverAll(v any, prefixes ...string) map[string][]MethodInfo {
	result := make(map[string][]MethodInfo)
	for _, prefix := range prefixes {
		methods := d.Discover(v, prefix)
		if len(methods) > 0 {
			result[prefix] = methods
		}
	}
	return result
}

// HasMethod checks if a specific method exists.
func (d *Discovery) HasMethod(v any, name string) bool {
	val := reflect.ValueOf(v)
	method := val.MethodByName(name)
	return method.IsValid()
}

// Call invokes a method by name with optional arguments.
func (d *Discovery) Call(v any, name string, args ...any) ([]any, error) {
	val := reflect.ValueOf(v)
	method := val.MethodByName(name)
	if !method.IsValid() {
		return nil, nil
	}

	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		in[i] = reflect.ValueOf(arg)
	}

	out := method.Call(in)
	result := make([]any, len(out))
	for i, v := range out {
		result[i] = v.Interface()
	}
	return result, nil
}

// CallWithContext invokes a method with context as first argument.
func (d *Discovery) CallWithContext(ctx context.Context, v any, name string, args ...any) ([]any, error) {
	allArgs := make([]any, 0, len(args)+1)
	allArgs = append(allArgs, ctx)
	allArgs = append(allArgs, args...)
	return d.Call(v, name, allArgs...)
}
