package contracts

import (
	"fmt"
	"reflect"
)

// Implements is a zero-cost marker used to declare that a struct implements an interface T.
//
// Usage:
//
//	type MyService struct {
//		contracts.Implements[IService]
//	}
//
// This enables:
// 1. IDE navigation: Clicking IService takes you to the interface definition.
// 2. IDE discovery: "Find Usages" on IService will show MyService.
// 3. Runtime validation: contracts.Verify(&MyService{}) checks if the implementation is valid.
type Implements[T any] struct{}

// validator is an internal interface used by Verify to identify contract markers.
type validator interface {
	validateContract(any) error
}

// validateContract implements the validator interface.
func (i Implements[T]) validateContract(v any) error {
	if _, ok := v.(T); !ok {
		// Get the type of T for error reporting
		var t *T
		target := reflect.TypeOf(t).Elem()
		return fmt.Errorf("contract violation: %T does not implement %v", v, target)
	}
	return nil
}

// Verify checks if the given instance satisfies all its declared contracts.
// It scans for fields of type contracts.Implements[T] and validates them.
func Verify(v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Invalid {
		return nil
	}

	typ := val.Type()
	// Contract markers are usually placed in the struct, but we validate against the pointer
	// if the instance is a pointer, because method sets often depend on the pointer receiver.
	baseTyp := typ
	if baseTyp.Kind() == reflect.Ptr {
		baseTyp = baseTyp.Elem()
	}

	if baseTyp.Kind() != reflect.Struct {
		return nil
	}

	for i := 0; i < baseTyp.NumField(); i++ {
		field := baseTyp.Field(i)
		
		// We check if the field type implements our internal validator interface.
		// Since Implements[T] has a value receiver for validateContract, 
		// both the type and its pointer will implement it.
		if field.Type.Implements(reflect.TypeOf((*validator)(nil)).Elem()) {
			// Instantiate the field type to call validateContract.
			// Since it's a zero-sized struct, this is cheap.
			vld := reflect.New(field.Type).Elem().Interface().(validator)
			if err := vld.validateContract(v); err != nil {
				return err
			}
		}
	}

	return nil
}

// MustVerify is like Verify but panics if a contract is violated.
func MustVerify(v any) {
	if err := Verify(v); err != nil {
		panic(err)
	}
}
