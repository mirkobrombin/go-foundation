//go:build !run_foundation_doctor

package app

import "testing"

func TestRunDoctorDisabled(t *testing.T) {
	a := New()

	if err := a.runDoctor(); err != nil {
		t.Fatalf("runDoctor() error = %v", err)
	}
}
