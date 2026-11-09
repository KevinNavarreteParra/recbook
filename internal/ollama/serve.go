package ollama

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

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

	cmd := exec.Command(binary, "serve")
	cmd.Env = append(os.Environ(), "OLLAMA_HOST="+host)
	cmd.Stdout, cmd.Stderr = nil, nil // its logs are noise here; run `ollama serve` by hand to see them
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s serve: %w", binary, err)
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

// Stop shuts down a server that Serve started, giving it ten seconds to
// unload models and exit.
func (s *Server) Stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	s.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		s.cmd.Process.Kill()
		<-done
	}
}
