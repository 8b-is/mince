// Package wrap launches an agent with a private ephemeral mince proxy.
package wrap

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"mince/internal/ccr"
	"mince/internal/proxy"
)

// Run starts a proxy on 127.0.0.1:port, injects base URLs, and executes the
// agent. It returns the child's exit code.
func Run(agent string, args []string, port int, store *ccr.Store, logger *slog.Logger) (int, error) {
	if agent == "" {
		return 2, fmt.Errorf("wrap: no command given")
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return 2, fmt.Errorf("wrap: listen: %w", err)
	}
	cfg := proxy.Config{
		Addr:        l.Addr().String(),
		Store:       store,
		Logger:      logger,
		MinBytes:    1024,
		ProtectTail: 2,
	}
	srv := proxy.New(cfg)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(l) }()

	base := "http://" + l.Addr().String()
	cmd := exec.Command(agent, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	env := os.Environ()
	env = append(env,
		"ANTHROPIC_BASE_URL="+base,
		"OPENAI_BASE_URL="+base+"/v1",
		"OPENAI_API_BASE="+base+"/v1",
	)
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		_ = l.Close()
		return 2, fmt.Errorf("wrap: start %s: %w", agent, err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigCh:
			_ = cmd.Process.Signal(sig)
		case <-done:
		}
	}()

	waitErr := cmd.Wait()
	close(done)
	signal.Stop(sigCh)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.Close()
	select {
	case <-serveErr:
	case <-ctx.Done():
	}

	if waitErr == nil {
		return 0, nil
	}
	if ee, ok := waitErr.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 1, waitErr
}
