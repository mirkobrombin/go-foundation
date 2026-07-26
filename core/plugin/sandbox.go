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

const (
	maxReadyMessageSize = 64 << 10
	defaultStopTimeout  = 5 * time.Second
)

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

	decoder := json.NewDecoder(io.LimitReader(e.out, maxReadyMessageSize+1))
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
		if err == nil {
			return nil
		}
		return errors.Join(err, e.killAndWait())
	case <-startCtx.Done():
		return errors.Join(startCtx.Err(), e.killAndWait())
	}
}

// Stop sends a stop command and waits for the external process to exit.
func (e *ExecSandbox) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultStopTimeout)
	defer cancel()
	return e.StopContext(ctx)
}

// StopContext sends a stop command and enforces the supplied shutdown deadline.
func (e *ExecSandbox) StopContext(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd.Process == nil || e.cmd.ProcessState != nil {
		return errors.New("plugin: sandbox not started")
	}

	done := make(chan error, 1)
	go func() {
		if err := json.NewEncoder(e.in).Encode(map[string]any{"cmd": "stop"}); err != nil {
			done <- errors.Join(err, e.killAndWait())
			return
		}
		done <- e.cmd.Wait()
	}()

	select {
	case err := <-done:
		e.closePipes()
		return err
	case <-ctx.Done():
		_ = e.cmd.Process.Kill()
		_ = e.in.Close()
		err := <-done
		e.closePipes()
		return errors.Join(ctx.Err(), err)
	}
}

func (e *ExecSandbox) killAndWait() error {
	if e.cmd.Process == nil || e.cmd.ProcessState != nil {
		e.closePipes()
		return nil
	}
	killErr := e.cmd.Process.Kill()
	waitErr := e.cmd.Wait()
	e.closePipes()
	return errors.Join(killErr, waitErr)
}

func (e *ExecSandbox) closePipes() {
	if e.in != nil {
		_ = e.in.Close()
	}
	if e.out != nil {
		_ = e.out.Close()
	}
}
