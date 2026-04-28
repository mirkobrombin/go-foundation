package errutil

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

var skipPackages = []string{
	"github.com/mirkobrombin/go-foundation/pkg/errutil.",
	"runtime.",
}

var mu sync.RWMutex

// SetSkipPackages appends packages to the skip list for stack trace filtering.
func SetSkipPackages(pkgs ...string) {
	mu.Lock()
	defer mu.Unlock()
	skipPackages = append(skipPackages, pkgs...)
}

func isSkip(pkg string) bool {
	mu.RLock()
	defer mu.RUnlock()
	for _, s := range skipPackages {
		if strings.HasPrefix(pkg, s) {
			return true
		}
	}
	return false
}

type withStack struct {
	err       error
	stack     []uintptr
	showTrace bool
}

func (w *withStack) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			fmt.Fprint(s, w.err.Error())
			w.writeVerbose(s)
			return
		}
		fallthrough
	case 's':
		fmt.Fprint(s, w.err.Error())
	case 'q':
		fmt.Fprintf(s, "%q", w.err.Error())
	}
}

func (w *withStack) Error() string {
	if w.showTrace {
		var sb strings.Builder
		sb.WriteString(w.err.Error())
		w.writeChain(&sb, map[*withStack]bool{})
		return sb.String()
	}
	return w.err.Error()
}

func (w *withStack) Clean() string {
	return w.err.Error()
}

func (w *withStack) writeVerbose(wr fmt.State) {
	frames := runtime.CallersFrames(w.stack)
	for {
		frame, more := frames.Next()
		if frame.Func != nil {
			fn := frame.Func.Name()
			if !isSkip(fn) {
				fmt.Fprintf(wr, "\n    at %s:%d (%s)", frame.File, frame.Line, fn)
			}
		}
		if !more {
			break
		}
	}
}

func (w *withStack) writeStack(sb *strings.Builder) {
	frames := runtime.CallersFrames(w.stack)
	for {
		frame, more := frames.Next()
		if frame.Func != nil {
			fn := frame.Func.Name()
			if !isSkip(fn) {
				sb.WriteString(fmt.Sprintf("\n    at %s:%d (%s)", frame.File, frame.Line, fn))
			}
		}
		if !more {
			break
		}
	}
}

func (w *withStack) writeChain(sb *strings.Builder, seen map[*withStack]bool) {
	if seen[w] {
		return
	}
	seen[w] = true
	w.writeStack(sb)
	if next, ok := w.err.(*withStack); ok {
		next.writeChain(sb, seen)
	}
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

type withTraceOnly struct {
	stack   []uintptr
	message string
}

func (w *withTraceOnly) Error() string {
	var sb strings.Builder
	sb.WriteString(w.message)
	sb.WriteString("\n")
	sb.Write(w.panicStack())
	return sb.String()
}

func (w *withTraceOnly) panicStack() []byte {
	frames := runtime.CallersFrames(w.stack)
	var sb strings.Builder
	for {
		frame, more := frames.Next()
		if frame.Func != nil {
			fn := frame.Func.Name()
			if !isSkip(fn) {
				sb.WriteString(fmt.Sprintf("\n    at %s:%d (%s)", frame.File, frame.Line, fn))
			}
		}
		if !more {
			break
		}
	}
	return []byte(sb.String())
}

// Wrap captures a stack trace at the call site and attaches it to err.
// Error() returns only the wrapped message. Use %+v or WError for full trace.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*withStack); ok {
		return err
	}
	pc := make([]uintptr, 32)
	n := runtime.Callers(2, pc)
	return &withStack{err: err, stack: pc[:n]}
}

// WError is an opinionated Wrap that includes the stack trace directly in
// Error(). Use it when you always want the trace visible on every print.
func WError(err error) error {
	if err == nil {
		return nil
	}
	pc := make([]uintptr, 32)
	n := runtime.Callers(2, pc)
	return &withStack{err: err, stack: pc[:n], showTrace: true}
}

// WithCode wraps an error with a string code, e.g. [CODE] message.
func WithCode(code string, err error) error {
	if err == nil {
		return nil
	}
	return &withCode{err: err, code: code}
}

// StackTrace unwraps err and returns the captured stack frames, if any.
func StackTrace(err error) []uintptr {
	type stackTracer interface {
		Stack() []uintptr
	}
	var st stackTracer
	for {
		if s, ok := err.(stackTracer); ok {
			st = s
		}
		if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrapper.Unwrap()
		} else {
			break
		}
	}
	if st != nil {
		return st.Stack()
	}
	return nil
}

// MultiError aggregates multiple errors into one.
type MultiError struct {
	Errors []error
}

// Append adds non-nil errors to the collection.
func (e *MultiError) Append(errs ...error) {
	for _, err := range errs {
		if err != nil {
			e.Errors = append(e.Errors, err)
		}
	}
}

// Error returns a concatenated error message.
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

// Unwrap returns the collected errors for errors.Is/As traversal.
func (e *MultiError) Unwrap() []error {
	return e.Errors
}

// HasErrors reports whether any errors were collected.
func (e *MultiError) HasErrors() bool {
	return len(e.Errors) > 0
}

// ErrorOrNil returns the MultiError if errors exist, or nil.
func (e *MultiError) ErrorOrNil() error {
	if len(e.Errors) == 0 {
		return nil
	}
	return e
}

// JoinErrors merges multiple errors into a single MultiError. Returns nil if none.
func JoinErrors(errs ...error) error {
	e := &MultiError{}
	e.Append(errs...)
	return e.ErrorOrNil()
}

// Recover catches a panic in fn, prints a clean stack trace to stderr with
// file:line and function names, then exits with status 1. The panic always
// terminates the process.
func Recover(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			lines := strings.Split(string(stack), "\n")

			var out strings.Builder
			for _, l := range lines {
				trimmed := strings.TrimSpace(l)
				if trimmed == "" {
					continue
				}
				if strings.HasPrefix(trimmed, "goroutine ") && strings.Contains(trimmed, "[running]") {
					continue
				}
				if strings.Contains(l, "runtime.") || strings.Contains(l, "runtime/") {
					continue
				}
				if strings.Contains(l, "pkg/errutil/") {
					continue
				}
				out.WriteString(l)
				out.WriteString("\n")
			}

			prefix := fmt.Sprintf("panic: %v\n", r)
			fmt.Fprint(os.Stderr, prefix+out.String())
			os.Exit(1)
		}
	}()
	fn()
}

// Trace captures a stack trace at this point without modifying or wrapping
// an existing error. Useful in defers to annotate "where did this come from".
func Trace(msg string) error {
	pc := make([]uintptr, 32)
	n := runtime.Callers(2, pc)
	return &withTraceOnly{message: msg, stack: pc[:n]}
}