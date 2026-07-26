package plugin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecSandboxCleansUpAfterStartTimeout(t *testing.T) {
	sandbox := NewExecSandbox("/bin/sh", "-c", "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := sandbox.Start(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start() error = %v, want deadline exceeded", err)
	}
	if sandbox.cmd.ProcessState == nil {
		t.Fatal("timed out sandbox process was not reaped")
	}
}

func TestExecSandboxStopContextKillsUnresponsiveProcess(t *testing.T) {
	sandbox := NewExecSandbox(
		"/bin/sh",
		"-c",
		`printf '{"ready":true}\n'; sleep 30`,
	)
	if err := sandbox.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := sandbox.StopContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StopContext() error = %v, want deadline exceeded", err)
	}
	if sandbox.cmd.ProcessState == nil {
		t.Fatal("unresponsive sandbox process was not reaped")
	}
}

func TestExecSandboxReapsProcessWhenStopWriteFails(t *testing.T) {
	sandbox := NewExecSandbox(
		"/bin/sh",
		"-c",
		`printf '{"ready":true}\n'; exec 0<&-; sleep 30`,
	)
	if err := sandbox.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)

	if err := sandbox.Stop(); err == nil {
		t.Fatal("Stop() succeeded after the child closed stdin")
	}
	if sandbox.cmd.ProcessState == nil {
		t.Fatal("sandbox process was not reaped after stop write failure")
	}
}
