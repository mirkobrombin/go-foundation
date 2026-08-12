package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestStampWins(t *testing.T) {
	previous := stamped
	t.Cleanup(func() { stamped = previous })

	stamped = "  v2.2.0  "
	if got := Current(); got != "v2.2.0" {
		t.Fatalf("Current() = %q, want v2.2.0", got)
	}
	if !Released() {
		t.Fatal("a stamped build is a released build")
	}
}

// TestUnstampedBuildSaysSo covers the case that matters: a binary that cannot
// name itself must say so rather than report a plausible number.
func TestUnstampedBuildSaysSo(t *testing.T) {
	previous := stamped
	t.Cleanup(func() { stamped = previous })
	stamped = ""

	got := Current()
	if got == "" {
		t.Fatal("Current() must never be empty")
	}
	// Under go test the main module has no version, so this is a working copy.
	if !strings.HasPrefix(got, devel) {
		t.Fatalf("Current() = %q, want a development marker", got)
	}
	if Released() {
		t.Fatal("a working copy must not claim to be released")
	}
}

func TestShortRevisionIsReadable(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "1c895267d2f4a0b9c1e2"},
		{Key: "vcs.modified", Value: "true"},
	}}
	if got := shortRevision(info); got != "1c89526-dirty" {
		t.Fatalf("shortRevision() = %q", got)
	}

	clean := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "1c895267d2f4a0b9c1e2"},
		{Key: "vcs.modified", Value: "false"},
	}}
	if got := shortRevision(clean); got != "1c89526" {
		t.Fatalf("shortRevision() = %q", got)
	}

	if got := shortRevision(&debug.BuildInfo{}); got != "" {
		t.Fatalf("shortRevision() with no VCS info = %q, want empty", got)
	}
}
