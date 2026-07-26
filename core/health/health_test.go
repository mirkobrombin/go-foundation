package health

import (
	"context"
	"testing"
)

type checkFunc func(context.Context) Report

func (f checkFunc) Check(ctx context.Context) Report {
	return f(ctx)
}

func TestStatusString(t *testing.T) {
	tests := map[Status]string{
		StatusHealthy:   "healthy",
		StatusDegraded:  "degraded",
		StatusUnhealthy: "unhealthy",
		Status(99):      "unknown",
	}
	for status, want := range tests {
		if got := status.String(); got != want {
			t.Fatalf("Status.String() = %q, want %q", got, want)
		}
	}
}

func TestRegistryCheckAll(t *testing.T) {
	reg := NewRegistry()
	reg.Register("db", checkFunc(func(ctx context.Context) Report {
		return Report{Status: StatusHealthy, Details: map[string]any{"ok": true}}
	}))

	results := reg.CheckAll(context.Background())
	report, ok := results["db"]
	if !ok {
		t.Fatal("CheckAll() missing db report")
	}
	if report.Status != StatusHealthy {
		t.Fatalf("Status = %v, want healthy", report.Status)
	}
	if report.Duration == 0 {
		t.Fatal("Duration was not set")
	}
	if report.Details["ok"] != true {
		t.Fatalf("Details[ok] = %v, want true", report.Details["ok"])
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	checker := checkFunc(func(context.Context) Report {
		return Report{Status: StatusHealthy}
	})
	var typedNil *nilChecker

	tests := []struct {
		name    string
		checker Checker
		setup   func(*Registry)
	}{
		{name: "", checker: checker},
		{name: "nil", checker: nil},
		{name: "typed-nil", checker: typedNil},
		{
			name:    "duplicate",
			checker: checker,
			setup: func(registry *Registry) {
				registry.Register("duplicate", checker)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			if test.setup != nil {
				test.setup(registry)
			}
			defer func() {
				if recover() == nil {
					t.Fatal("Register() accepted an invalid checker")
				}
			}()
			registry.Register(test.name, test.checker)
		})
	}
}

type nilChecker struct{}

func (*nilChecker) Check(context.Context) Report {
	return Report{}
}
