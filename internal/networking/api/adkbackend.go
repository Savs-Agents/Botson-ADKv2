// StartBackend runs a real Google ADK v2 REST+A2A server (via
// google.golang.org/adk/v2/cmd/launcher/prod) internally; adkreverseproxy.go
// fronts it with a plain HTTP reverse proxy so this package's own Server
// (server.go) can expose ADK's stock REST/A2A surface without
// reimplementing any of it by hand.
package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/prod"
)

// readyTimeout bounds how long StartBackend waits for the backend to become
// reachable before giving up.
const readyTimeout = 15 * time.Second

// serverWriteTimeout and serverIdleTimeout override the launcher's own
// defaults (15s write, 60s idle -- see cmd/launcher/web/web.go's flag
// registration in the vendored ADK module). Go's http.Server.WriteTimeout
// bounds the entire handler execution, not just byte-writing -- for
// /api/run, the handler runs the whole agent turn (every LLM round trip and
// tool call) before writing anything, so the 15s default silently severs
// the connection mid-turn for any real multi-tool-call turn. That produces
// a confusing symptom one layer up: the proxy's http.Client.Do sees the
// killed connection as a plain EOF, and the backend's own log shows
// "superfluous response.WriteHeader call" from the encode that failed
// against the now-dead connection. serverIdleTimeout is bumped alongside
// it since the proxy's http.DefaultClient pools keep-alive connections
// with a 90s IdleConnTimeout -- if the server's idle timeout were shorter,
// it could close a connection the client still thinks is live, causing the
// same EOF for a request that just happened to follow an idle gap.
const (
	serverWriteTimeout = 10 * time.Minute
	serverIdleTimeout  = 10 * time.Minute
)

// backend is a running ADK prod launcher instance.
type backend struct {
	baseURL string
	done    chan struct{}
	err     error // valid only after done is closed
}

// StartBackend launches prod.NewLauncher() against cfg on port (port==0
// picks a free ephemeral port) with the REST API and A2A sublaunchers
// activated. It blocks until the backend responds to a readiness probe, the
// launcher exits early (e.g. a missing AgentLoader), ctx is done, or
// readyTimeout elapses.
//
// Note: the underlying ADK launcher (cmd/launcher/web) always binds its
// http.Server to ":<port>" -- all interfaces, not loopback-only -- and has
// no flag or hook for restricting that or adding auth. This backend's port
// is picked freely/never advertised, but it is not itself network-isolated
// by ADK; this package's own bearer-token auth (auth.go) in front of the
// reverse proxy (adkreverseproxy.go) is what actually gates real traffic,
// not this bind behavior. See docs/process-architecture.md for the
// accepted residual risk this leaves (anything that can already reach the
// host machine can still reach this backend's port directly,
// unauthenticated).
//
// The launcher keeps running until ctx is cancelled; callers are responsible
// for cancelling ctx to shut it down.
func StartBackend(ctx context.Context, cfg launcher.Config, port int) (*backend, error) {
	if port == 0 {
		p, err := freePort()
		if err != nil {
			return nil, fmt.Errorf("adk backend: pick free port: %w", err)
		}
		port = p
	}

	b := &backend{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		done:    make(chan struct{}),
	}

	args := []string{
		"-port", strconv.Itoa(port),
		"-write-timeout", serverWriteTimeout.String(),
		"-idle-timeout", serverIdleTimeout.String(),
		"api", "a2a",
	}
	go func() {
		b.err = prod.NewLauncher().Execute(ctx, &cfg, args)
		close(b.done)
	}()

	if err := b.awaitReady(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

// BaseURL returns the backend's local HTTP base URL, e.g.
// "http://127.0.0.1:8080".
func (b *backend) BaseURL() string {
	return b.baseURL
}

// Done returns a channel that is closed once the underlying launcher has
// exited (normally via context cancellation, or with an error).
func (b *backend) Done() <-chan struct{} {
	return b.done
}

// Err returns the launcher's terminal error. It is only meaningful after
// Done() has been closed; it returns nil beforehand.
func (b *backend) Err() error {
	select {
	case <-b.done:
		return b.err
	default:
		return nil
	}
}

func (b *backend) awaitReady(ctx context.Context) error {
	readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(readyCtx, http.MethodGet, b.baseURL+"/api/list-apps", nil)
		if err == nil {
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
				return nil
			}
		}

		select {
		case <-b.done:
			if b.err != nil {
				return fmt.Errorf("adk backend: launcher exited before becoming ready: %w", b.err)
			}
			return fmt.Errorf("adk backend: launcher exited before becoming ready")
		case <-readyCtx.Done():
			return fmt.Errorf("adk backend: timed out waiting for readiness at %s: %w", b.baseURL, readyCtx.Err())
		case <-ticker.C:
		}
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
