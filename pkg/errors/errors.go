package errors

import (
	"fmt"
	"runtime"
	"strings"
)

type MultiError struct {
	Errors []error
}

type withStack struct {
	err   error
	stack []uintptr
}

func (w *withStack) Error() string {
	return w.err.Error()
}

func (w *withStack) Unwrap() error {
	return w.err
}

func (w *withStack) Stack() []uintptr {
	return w.stack
}

type withCode struct {
	err  error
	code string
}

func (w *withCode) Error() string {
	return fmt.Sprintf("[%s] %s", w.code, w.err.Error())
}

func (w *withCode) Unwrap() error {
	return w.err
}

func (w *withCode) Code() string {
	return w.code
}

func Wrap(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*withStack); ok {
		return err
	}
	pc := make([]uintptr, 32)
	n := runtime.Callers(3, pc)
	return &withStack{err: err, stack: pc[:n]}
}

func WithCode(code string, err error) error {
	if err == nil {
		return nil
	}
	return &withCode{err: err, code: code}
}

func StackTrace(err error) []uintptr {
	type stackTracer interface {
		Stack() []uintptr
	}
	var st stackTracer
	for {
		if s, ok := err.(stackTracer); ok {
			st = s
		}
		u := err
		if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrapper.Unwrap()
		} else {
			break
		}
		_ = u
	}
	if st != nil {
		return st.Stack()
	}
	return nil
}

func (e *MultiError) Append(errs ...error) {
	for _, err := range errs {
		if err != nil {
			e.Errors = append(e.Errors, err)
		}
	}
}

func (e *MultiError) Error() string {
	if len(e.Errors) == 0 {
		return ""
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	var sb strings.Builder
	sb.WriteString("multiple errors occurred: ")
	for i, err := range e.Errors {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(err.Error())
	}
	return sb.String()
}

func (e *MultiError) Unwrap() []error {
	return e.Errors
}

func (e *MultiError) HasErrors() bool {
	return len(e.Errors) > 0
}

func (e *MultiError) ErrorOrNil() error {
	if len(e.Errors) == 0 {
		return nil
	}
	return e
}

func Join(errs ...error) error {
	e := &MultiError{}
	e.Append(errs...)
	return e.ErrorOrNil()
}
