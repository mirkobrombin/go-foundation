//go:build run_foundation_doctor

package web

import "testing"

func TestRunDoctorEnabled(t *testing.T) {
	t.Setenv("FOUNDATION_DOCTOR", "fail")

	s := New()
	s.MapGet("/ping", func(ctx *Context) error {
		ctx.String(200, "pong")
		return nil
	})

	if err := s.runDoctor(); err != nil {
		t.Fatalf("runDoctor() error = %v", err)
	}
}

func TestRunDoctorEnabledFails(t *testing.T) {
	t.Setenv("FOUNDATION_DOCTOR", "fail")

	s := New()
	if err := s.runDoctor(); err == nil {
		t.Fatal("runDoctor() error = nil, want error")
	}
}
