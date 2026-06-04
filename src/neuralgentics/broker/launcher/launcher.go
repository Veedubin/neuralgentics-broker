package launcher

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"neuralgentics-broker/src/neuralgentics/broker/registry"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// HealthStatus represents the health state of a server process.
type HealthStatus string

const (
	// HealthStopped means the server is not running or has no pipes.
	HealthStopped HealthStatus = "stopped"
	// HealthDead means the process exists but is unresponsive (e.g. SIGKILL pending).
	HealthDead HealthStatus = "dead"
	// HealthInitializing means the process is alive but the handshake is not yet complete.
	HealthInitializing HealthStatus = "initializing"
	// HealthHealthy means the process is alive and pipes are open.
	HealthHealthy HealthStatus = "healthy"
	// HealthUnhealthy means the process is alive but ping or initialize failed.
	HealthUnhealthy HealthStatus = "unhealthy"
)

// Launcher manages MCP server subprocess lifecycles.
type Launcher struct {
	registry *registry.Registry
}

// NewLauncher creates a Launcher that operates on the given registry.
func NewLauncher(reg *registry.Registry) *Launcher {
	return &Launcher{
		registry: reg,
	}
}

// Start launches an MCP server subprocess based on its config type.
// For stdio servers, it captures stdin/stdout pipes and stores the
// process handle in the registry.
func (l *Launcher) Start(config types.ServerConfig) error {
	entry, ok := l.registry.Get(config.Name)
	if !ok {
		return fmt.Errorf("server %q not registered", config.Name)
	}

	cmd, stdinPipe, stdoutPipe, err := buildCommand(config)
	if err != nil {
		return fmt.Errorf("build command for %q: %w", config.Name, err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server %q: %w", config.Name, err)
	}

	entry.Process = cmd.Process
	entry.Stdin = stdinPipe
	entry.Stdout = stdoutPipe

	l.registry.UpdateEntry(config.Name, entry)

	// Monitor the process for unexpected exits.
	go func() {
		state, _ := cmd.Process.Wait()
		if state != nil && !state.Success() {
			l.clearAfterExit(config.Name)
		}
	}()

	return nil
}

// Stop terminates a running MCP server process. It sends Interrupt first,
// then Kill after a 5-second timeout.
func (l *Launcher) Stop(name string) error {
	entry, ok := l.registry.Get(name)
	if !ok {
		return fmt.Errorf("server %q not found", name)
	}

	if entry.Process == nil {
		return nil
	}

	if err := entry.Process.Signal(os.Interrupt); err != nil {
		// Fall back to Kill if interrupt fails.
		_ = entry.Process.Kill()
	}

	// Give the process time to shut down gracefully.
	done := make(chan error, 1)
	go func() {
		_, waitErr := entry.Process.Wait()
		done <- waitErr
	}()

	select {
	case <-done:
		// Process exited.
	case <-time.After(5 * time.Second):
		// Force kill.
		_ = entry.Process.Kill()
	}

	l.clearAfterExit(name)

	return nil
}

// Health checks if a server process is still alive.
// Returns a HealthStatus string indicating the process state:
//   - "stopped":     no process or no registry entry
//   - "dead":        process.Signal(nil) returns error (process no longer exists)
//   - "initializing": process alive but pipes not yet connected (stdio)
//   - "healthy":     process alive and both pipes are open
func (l *Launcher) Health(name string) HealthStatus {
	entry, ok := l.registry.Get(name)
	if !ok {
		return HealthStopped
	}

	if entry.Process == nil {
		return HealthStopped
	}

	// Send signal 0 to check if the process still exists.
	// syscall.Signal(0) is the POSIX signal-zero check: it verifies the
	// OS process exists without actually sending a signal. Using
	// os.Signal(nil) is incorrect as it produces "os: unsupported signal type".
	if entry.Process.Signal(syscall.Signal(0)) != nil {
		return HealthDead
	}

	// Check that both stdin and stdout pipes are present for stdio servers.
	if entry.Stdin == nil || entry.Stdout == nil {
		return HealthInitializing
	}

	return HealthHealthy
}

// clearAfterExit clears the running state after a process exits.
func (l *Launcher) clearAfterExit(name string) {
	entry, ok := l.registry.Get(name)
	if !ok {
		return
	}
	entry.Process = nil
	entry.Stdin = nil
	entry.Stdout = nil
	// Tools are preserved for status reporting after exit.
	l.registry.UpdateEntry(name, entry)
}

// buildCommand constructs an exec.Cmd for the given server config.
// For stdio-type servers, it sets up stdin/stdout pipes.
func buildCommand(config types.ServerConfig) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	cmd := exec.Command(config.Command, config.Args...)

	// Set environment variables.
	if len(config.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range config.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	var stdinPipe io.WriteCloser
	var stdoutPipe io.ReadCloser
	var err error

	switch config.Type {
	case "stdio":
		stdinPipe, err = cmd.StdinPipe()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("stdin pipe: %w", err)
		}
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
		}
	case "http", "sse":
		// HTTP/SSE servers don't need stdio pipes.
		// They will be handled by the proxy layer in a future phase.
	default:
		return nil, nil, nil, fmt.Errorf("unknown server type: %q", config.Type)
	}

	return cmd, stdinPipe, stdoutPipe, nil
}
