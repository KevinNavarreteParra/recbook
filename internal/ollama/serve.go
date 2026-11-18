package ollama

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// CPUHost is where ServeCPU listens: beside DefaultHost, so that a server
// on the CPU is never mistaken for one on the GPU, or the other way round.
const CPUHost = "127.0.0.1:11435"

// Server is an Ollama server that a job is using.
type Server struct {
	*Client
	cmd *exec.Cmd // nil when the server was already running and isn't ours
}

// Serve connects to the server at host, starting one from binary if none is
// running. Stop must be called when the job is done: it stops a server that
// Serve started and leaves one that was already running alone.
func Serve(ctx context.Context, binary, host string) (*Server, error) {
	client := NewClient(host)
	if client.Ready(ctx) {
		return &Server{Client: client}, nil
	}
	return start(ctx, client, exec.Command(binary, "serve"), host)
}

// ServeCPU starts a server of its own at host that can't use the GPU, so
// that embedding is safe by day, while the GPU may be running a game.
//
// Nothing short of hiding the GPU is sure to keep a server off it: Ollama
// 0.40 ignored CUDA_VISIBLE_DEVICES=-1 (most likely because it chooses the
// GPUs for its runners itself), and llama.cpp can move large batches to a
// GPU even with no layers on it, so a request's num_gpu of 0 isn't enough
// either. So the server runs in a bubblewrap sandbox (bwrap) that sees the
// whole filesystem and network but has a /dev of its own, without the
// GPU's device files, and at the lowest CPU priority, so a game keeps the
// processor. It refuses to use a server already running at host, which
// might have the GPU.
func ServeCPU(ctx context.Context, binary, host string) (*Server, error) {
	client := NewClient(host)
	if client.Ready(ctx) {
		return nil, fmt.Errorf("a server is already running on %s, and it may be using the GPU", host)
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		return nil, fmt.Errorf("embedding on the CPU needs bubblewrap (bwrap) to hide the GPU: %w", err)
	}
	return start(ctx, client, withoutGPU(binary, "serve"), host)
}

// withoutGPU is the command name args, in a sandbox with no GPU devices to
// open and at the lowest CPU priority (see ServeCPU).
func withoutGPU(name string, args ...string) *exec.Cmd {
	sandbox := []string{"-n", "19", "bwrap", "--dev-bind", "/", "/", "--dev", "/dev", "--", name}
	return exec.Command("nice", append(sandbox, args...)...)
}

// start runs cmd, a server for host, and waits for it to answer.
func start(ctx context.Context, client *Client, cmd *exec.Cmd, host string) (*Server, error) {
	cmd.Env = append(os.Environ(), "OLLAMA_HOST="+host)
	cmd.Stdout, cmd.Stderr = nil, nil // its logs are noise here; run `ollama serve` by hand to see them
	// A process group of its own, so that Stop reaches the server's model
	// runners, and anything wrapping it, as well as the server.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", strings.Join(cmd.Args, " "), err)
	}
	s := &Server{Client: client, cmd: cmd}

	deadline := time.Now().Add(60 * time.Second)
	for !client.Ready(ctx) {
		if time.Now().After(deadline) || ctx.Err() != nil {
			s.Stop()
			return nil, fmt.Errorf("ollama didn't come up on %s within a minute", host)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return s, nil
}

// Stop shuts down a server that Serve or ServeCPU started, giving it and
// its runners ten seconds to unload models and exit before killing them.
func (s *Server) Stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	group := -s.cmd.Process.Pid
	syscall.Kill(group, syscall.SIGTERM)
	done := make(chan struct{})
	go func() { s.cmd.Wait(); close(done) }()

	deadline := time.After(10 * time.Second)
	select {
	case <-done:
	case <-deadline:
		syscall.Kill(group, syscall.SIGKILL)
		<-done
		return
	}
	// The process we started is gone; its runners may still be exiting.
	for !errors.Is(syscall.Kill(group, 0), syscall.ESRCH) {
		select {
		case <-deadline:
			syscall.Kill(group, syscall.SIGKILL)
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
