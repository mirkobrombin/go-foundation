package options

import (
	"fmt"
	"reflect"
)

// Options wraps a validated configuration section for DI consumption.
// Similar to .NET IOptions<T>.
type Options[T any] struct {
	Value T
}

// Configure registers an Options[T] in the DI container by binding
// from the given configuration section at startup.
//
// Usage:
//
//	di.Configure[AppOptions](builder, cfg.GetSection("app"))
func Configure[T any](value T) Options[T] {
	return Options[T]{Value: value}
}

// MergeWithSemantics controls how Merge handles zero values.
type MergeWithSemantics int

const (
	// LastWins means the last config always overwrites, even with zero values.
	LastWins MergeWithSemantics = iota
	// SkipZero means zero values in later configs do not overwrite non-zero values.
	SkipZero
)

// MergeConfig combines configs with explicit merge semantics.
func MergeConfig[T any](semantics MergeWithSemantics, cfgs ...T) T {
	var result T
	resultVal := reflect.ValueOf(&result).Elem()
	resultType := resultVal.Type()

	for _, cfg := range cfgs {
		cfgVal := reflect.ValueOf(cfg)
		for i := 0; i < resultType.NumField(); i++ {
			field := cfgVal.Field(i)
			switch semantics {
			case LastWins:
				resultVal.Field(i).Set(field)
			case SkipZero:
				if !field.IsZero() {
					resultVal.Field(i).Set(field)
				}
			}
		}
	}
	return result
}

// MustValidate panics if the struct contains zero-valued required fields.
// Required fields are marked with `required:"true"` tag.
func MustValidate[T any](cfg T) {
	val := reflect.ValueOf(cfg)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Tag.Get("required") == "true" {
			if val.Field(i).IsZero() {
				panic(fmt.Sprintf("options: required field %s is zero", field.Name))
			}
		}
	}
}