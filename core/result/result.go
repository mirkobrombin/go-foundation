package result

// Result represents either a success value or an error.
//
// Example:
//
//	r := result.Ok(42)
//	if r.IsOk() { ... }
type Result[T any] struct {
	value T
	err   error
}

// Ok creates a successful Result.
func Ok[T any](value T) Result[T] {
	return Result[T]{value: value}
}

// Err creates an error Result.
func Err[T any](err error) Result[T] {
	return Result[T]{err: err}
}

// IsOk returns true if the result is successful.
func (r Result[T]) IsOk() bool {
	return r.err == nil
}

// IsErr returns true if the result contains an error.
func (r Result[T]) IsErr() bool {
	return r.err != nil
}

// Unwrap returns the value or panics if error.
//
// Notes:
//
// Panics if the result is an error.
func (r Result[T]) Unwrap() T {
	if r.err != nil {
		panic("called Unwrap on error result: " + r.err.Error())
	}
	return r.value
}

// UnwrapOr returns the value or the provided default.
func (r Result[T]) UnwrapOr(defaultValue T) T {
	if r.err != nil {
		return defaultValue
	}
	return r.value
}

// UnwrapOrElse returns the value or calls fn to get a default.
func (r Result[T]) UnwrapOrElse(fn func(error) T) T {
	if r.err != nil {
		return fn(r.err)
	}
	return r.value
}

// Error returns the error, or nil if successful.
func (r Result[T]) Error() error {
	return r.err
}

// Value returns the value and error (traditional Go style).
func (r Result[T]) Value() (T, error) {
	return r.value, r.err
}

// Map transforms the value if successful.
//
// Example:
//
//	r := result.Ok(10)
//	r2 := result.Map(r, func(i int) string { return strconv.Itoa(i) })
func Map[T, U any](r Result[T], fn func(T) U) Result[U] {
	if r.err != nil {
		return Err[U](r.err)
	}
	return Ok(fn(r.value))
}

// FlatMap transforms the value with a function that returns a Result.
func FlatMap[T, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if r.err != nil {
		return Err[U](r.err)
	}
	return fn(r.value)
}

// From wraps a traditional (value, error) tuple into a Result.
func From[T any](value T, err error) Result[T] {
	if err != nil {
		return Err[T](err)
	}
	return Ok(value)
}

// Try executes fn and wraps the result.
func Try[T any](fn func() (T, error)) Result[T] {
	v, err := fn()
	return From(v, err)
}

// Must panics if the result is an error, otherwise returns the value.
func Must[T any](r Result[T]) T {
	return r.Unwrap()
}
