//go:build run_foundation_doctor

package app

import "testing"

func TestRunDoctorEnabled(t *testing.T) {
	t.Setenv("FOUNDATION_DOCTOR", "fail")

	a := New()
	a.RegisterHTTP(&greetEndpoint{})
	if _, err := a.Build(); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if err := a.runDoctor(); err != nil {
		t.Fatalf("runDoctor() error = %v", err)
	}
}

func TestRunDoctorEnabledFails(t *testing.T) {
	t.Setenv("FOUNDATION_DOCTOR", "fail")

	a := New()
	if err := a.runDoctor(); err == nil {
		t.Fatal("runDoctor() error = nil, want error")
	}
}
