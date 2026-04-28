package errutil

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

var (
	yl = "\x1b[33m"
	cy = "\x1b[36m"
	gy = "\x1b[90m"
	rs = "\x1b[0m"
)

// Auto recovers from a panic, prints a rich stack trace, and exits.
func Auto() {
	if r := recover(); r != nil {
		drawRichTrace(r)
		os.Exit(1)
	}
}

// EnableAuto enables automatic panic recovery (stub for future use).
func EnableAuto() {}

func drawRichTrace(r any) {
	raw := debug.Stack()
	lines := strings.Split(string(raw), "\n")

	type entry struct {
		funcName string
		file     string
		line     int
	}

	var stack []entry

	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)

		if t == "" || strings.HasPrefix(t, "goroutine ") {
			continue
		}
		if strings.HasPrefix(t, "runtime.") || strings.Contains(t, "runtime/") {
			continue
		}
		if strings.Contains(t, "pkg/errutil/") || strings.HasPrefix(t, "created by ") {
			continue
		}
		if strings.Contains(t, "panic({") {
			continue
		}

		if !strings.Contains(t, ".go:") && !strings.HasPrefix(t, "created by ") {
			fn := strings.TrimSuffix(t, "(...)")
			fn = strings.TrimSuffix(fn, " {...}")

			e := entry{funcName: fn}

			if i+1 < len(lines) {
				nl := strings.TrimSpace(lines[i+1])
				if strings.Contains(nl, ".go:") {
					parts := strings.SplitN(nl, ".go:", 2)
					if len(parts) == 2 {
						e.file = strings.TrimSpace(parts[0]) + ".go"
						numStr := strings.TrimSpace(parts[1])
						if idx := strings.Index(numStr, " +"); idx != -1 {
							numStr = numStr[:idx]
						}
						n, err := strconv.Atoi(numStr)
						if err == nil {
							e.line = n
						}
					}
					i++
				}
			}

			if !strings.Contains(e.funcName, "drawRichTrace") && !strings.Contains(e.funcName, "Auto.func1") && !strings.HasPrefix(e.funcName, "Auto") && !strings.HasPrefix(e.funcName, "EnableAuto") && !strings.HasPrefix(e.funcName, "github.com/mirkobrombin/go-foundation/pkg/errutil.") {
				stack = append(stack, e)
			}
		}
	}

	yl := "\x1b[33m"
	cy := "\x1b[36m"
	gy := "\x1b[90m"
	rs := "\x1b[0m"

	fmt.Fprintf(os.Stderr, "\n  panic: %v\n\n", r)

	if len(stack) == 0 {
		fmt.Fprintln(os.Stderr, "  (no user frames)")
		return
	}

	for i := len(stack) - 1; i >= 0; i-- {
		e := stack[i]
		num := len(stack) - i

		fmt.Fprintf(os.Stderr, "  %s%d.%s %s%s%s\n", yl, num, rs, cy, e.funcName, rs)
		if e.file != "" {
			fmt.Fprintf(os.Stderr, "     %s%s:%d%s\n", gy, e.file, e.line, rs)
			snip(e.file, e.line, os.Stderr)
		}
		fmt.Fprintln(os.Stderr)
	}
}

func snip(path string, target int, w *os.File) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(w, "      %s(source not available)%s\n", gy, rs)
		return
	}
	defer f.Close()

	start := target - 2
	if start < 1 {
		start = 1
	}
	end := target + 2

	s := bufio.NewScanner(f)
	n := 0
	for s.Scan() {
		n++
		if n < start {
			continue
		}
		if n > end {
			break
		}
		mark := "   "
		if n == target {
			mark = " >>"
		}
		fmt.Fprintf(w, "      %s %5d  %s\n", mark, n, s.Text())
	}
}

// Print renders a rich trace (file:line + source context) for a wrapped error,
// following the same format as Auto().
func Print(err error) {
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "\n  error: %s\n\n", err.Error())

	type frame struct {
		funcName string
		file     string
		line     int
	}
	var chain []frame
	seen := map[*withStack]bool{}

	for e := err; e != nil; e = unwrapOne(e) {
		if ws, ok := e.(*withStack); ok && !seen[ws] {
			seen[ws] = true
			frms := runtime.CallersFrames(ws.stack)
			for {
				f, more := frms.Next()
				fn := f.Func.Name()
				if fn == "" {
					if !more {
						break
					}
					continue
				}
				if isSkip(fn) {
					if !more {
						break
					}
					continue
				}
				chain = append(chain, frame{funcName: fn[strings.LastIndex(fn, "/")+1:], file: f.File, line: f.Line})
				if !more {
					break
				}
			}
		}
	}

	for i := len(chain) - 1; i >= 0; i-- {
		f := chain[i]
		num := len(chain) - i
		fmt.Fprintf(os.Stderr, "  %s%d.%s %s%s%s\n", yl, num, rs, cy, f.funcName, rs)
		fmt.Fprintf(os.Stderr, "     %s%s:%d%s\n", gy, f.file, f.line, rs)
		snip(f.file, f.line, os.Stderr)
		fmt.Fprintln(os.Stderr)
	}
}

func unwrapOne(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}