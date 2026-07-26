//go:build !run_foundation_doctor

package web

import "testing"

func TestRunDoctorDisabled(t *testing.T) {
	s := New()

	if err := s.runDoctor(); err != nil {
		t.Fatalf("runDoctor() error = %v", err)
	}
}
