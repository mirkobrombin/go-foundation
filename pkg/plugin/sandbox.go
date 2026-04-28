package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

// ExecSandbox runs a plugin as an external process using a simple JSON-over-stdio protocol.
type ExecSandbox struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out io.ReadCloser
	mu  sync.Mutex
}

// NewExecSandbox creates a sandbox around the provided binary path and arguments.
func NewExecSandbox(path string, args ...string) *ExecSandbox {
	return &ExecSandbox{cmd: exec.Command(path, args...)}
}

// Start launches the external process and waits for a ready signal.
func (e *ExecSandbox) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd.Process != nil {
		return errors.New("plugin: sandbox already started")
	}

	in, err := e.cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := e.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	e.in = in
	e.out = out
	if err := e.cmd.Start(); err != nil {
		return err
	}

	decoder := json.NewDecoder(e.out)
	ready := make(chan error, 1)
	go func() {
		var msg map[string]any
		if err := decoder.Decode(&msg); err != nil {
			ready <- err
			return
		}
		if ok, _ := msg["ready"].(bool); ok {
			ready <- nil
			return
		}
		ready <- errors.New("plugin: unexpected ready message")
	}()

	startCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	select {
	case err := <-ready:
		return err
	case <-startCtx.Done():
		return startCtx.Err()
	}
}

// Stop sends a stop command and waits for the external process to exit.
func (e *ExecSandbox) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd.Process == nil {
		return errors.New("plugin: sandbox not started")
	}

	if err := json.NewEncoder(e.in).Encode(map[string]any{"cmd": "stop"}); err != nil {
		return err
	}
	return e.cmd.Wait()
}
