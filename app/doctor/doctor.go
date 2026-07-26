package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type Severity string

const (
	OK   Severity = "OK"
	Warn Severity = "WARN"
	Fail Severity = "FAIL"
)

type Check struct {
	Severity Severity `json:"severity"`
	Name     string   `json:"name"`
	Message  string   `json:"message"`
}

type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type Source struct {
	Routes []Route `json:"routes"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

func CheckSource(src Source) Report {
	var checks []Check

	checks = append(checks, checkRoutes(src.Routes)...)
	checks = append(checks, checkHealth(src.Routes))

	return Report{Checks: checks}
}

func (r Report) Failures() int {
	n := 0
	for _, check := range r.Checks {
		if check.Severity == Fail {
			n++
		}
	}
	return n
}

func (r Report) Warnings() int {
	n := 0
	for _, check := range r.Checks {
		if check.Severity == Warn {
			n++
		}
	}
	return n
}

func (r Report) WriteText(w io.Writer) error {
	if _, err := fmt.Fprintln(w, "foundation doctor"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for _, check := range r.Checks {
		if _, err := fmt.Fprintf(w, "%s %s: %s\n", check.Severity, check.Name, check.Message); err != nil {
			return err
		}
	}
	return nil
}

func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func Run(src Source) error {
	return RunWithEnv(src, os.Getenv("FOUNDATION_DOCTOR"), os.Stdout)
}

func RunWithEnv(src Source, mode string, w io.Writer) error {
	report := CheckSource(src)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "print"
	}

	switch mode {
	case "off":
		return nil
	case "json":
		return report.WriteJSON(w)
	case "fail":
		if err := report.WriteText(w); err != nil {
			return err
		}
		if failures := report.Failures(); failures > 0 {
			return fmt.Errorf("foundation doctor: %d failure(s)", failures)
		}
		return nil
	default:
		return report.WriteText(w)
	}
}

func checkRoutes(routes []Route) []Check {
	if len(routes) == 0 {
		return []Check{{
			Severity: Fail,
			Name:     "routes",
			Message:  "no routes registered",
		}}
	}

	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if seen[key] {
			return []Check{{
				Severity: Fail,
				Name:     "routes",
				Message:  "duplicate route " + key,
			}}
		}
		seen[key] = true
	}

	return []Check{{
		Severity: OK,
		Name:     "routes",
		Message:  fmt.Sprintf("%d route(s) registered", len(routes)),
	}}
}

func checkHealth(routes []Route) Check {
	paths := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.Method == "GET" {
			paths = append(paths, route.Path)
		}
	}
	sort.Strings(paths)

	hasLive := false
	hasReady := false
	for _, path := range paths {
		if path == "/health" || path == "/health/live" {
			hasLive = true
		}
		if path == "/health/ready" {
			hasReady = true
		}
	}

	if hasLive && hasReady {
		return Check{Severity: OK, Name: "health", Message: "liveness and readiness routes registered"}
	}
	if hasLive {
		return Check{Severity: Warn, Name: "health", Message: "liveness route registered without readiness route"}
	}
	return Check{Severity: Warn, Name: "health", Message: "no health route registered"}
}
