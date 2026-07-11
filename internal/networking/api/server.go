// Package api is Botson's single unified HTTP server: one http.Server,
// bound to a configurable host:port, serving ADK's own REST+A2A surface
// (reverse-proxied into an internally-run ADK backend, see
// adkbackend.go/adkreverseproxy.go) and Botson's own settings/agents/
// sessions/dashboard routes (routes.go) side by side, both behind one
// bearer-token auth middleware (auth.go). This replaced the former
// NATS-based split (a NATS gateway fronting adk.* over NATS, a separate
// NATS API fronting botson.* over NATS) in the 2026-07 move away from NATS
// as a transport.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/cmd/launcher"
)

// Config configures a Server.
type Config struct {
	// Host and Port are the address this server binds to -- the primary
	// interface any consumer talks to.
	Host string
	Port int

	// AuthToken gates every request via a bearer header; required.
	AuthToken string

	// ADK is the ADK launcher configuration passed through to the
	// internally-run ADK backend (adkbackend.go). ADK.AgentLoader is
	// required.
	ADK launcher.Config

	// BackendPort is the loopback port the internal ADK backend binds to;
	// 0 (the default) picks a free ephemeral port. This is never the same
	// port as Host:Port -- it's an implementation detail, not part of the
	// public interface.
	BackendPort int

	// RequestTimeout bounds how long the reverse proxy waits for the ADK
	// backend to start responding to a forwarded request; defaults to 8
	// minutes (real agent turns can run for minutes, not seconds -- see
	// adkbackend.go's serverWriteTimeout doc comment).
	RequestTimeout time.Duration

	// Logger is used for diagnostics; defaults to slog.Default().
	Logger *slog.Logger
}

// Server is Botson's unified HTTP API server.
type Server struct {
	cfg Config

	mu   sync.RWMutex
	addr string
}

// New validates cfg and returns a Server ready to Run.
func New(cfg Config) (*Server, error) {
	if cfg.AuthToken == "" {
		return nil, errors.New("api: Config.AuthToken is required")
	}
	if cfg.ADK.AgentLoader == nil {
		return nil, errors.New("api: Config.ADK.AgentLoader is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 8 * time.Minute
	}
	return &Server{cfg: cfg}, nil
}

// Run starts the internal ADK backend, builds the composed router (ADK
// reverse proxy + Botson's own REST routes, both behind the bearer-token
// auth middleware), and serves it on Config.Host:Port until ctx is
// cancelled or a fatal error occurs. On return, both the HTTP server and
// the backend have been shut down.
func (s *Server) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	backend, err := StartBackend(runCtx, s.cfg.ADK, s.cfg.BackendPort)
	if err != nil {
		return fmt.Errorf("api: start ADK backend: %w", err)
	}

	proxyHandler, err := NewReverseProxy(backend.BaseURL(), s.cfg.RequestTimeout, s.cfg.Logger)
	if err != nil {
		return fmt.Errorf("api: configure ADK reverse proxy: %w", err)
	}

	router := mux.NewRouter()
	router.PathPrefix("/api/").Handler(proxyHandler)
	router.PathPrefix("/a2a/").Handler(proxyHandler)
	router.Handle(a2asrv.WellKnownAgentCardPath, proxyHandler)
	Mount(router, &s.cfg.ADK)

	handler := authMiddleware(s.cfg.AuthToken, s.cfg.Logger)(router)

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port))
	if err != nil {
		return fmt.Errorf("api: listen on %s:%d: %w", s.cfg.Host, s.cfg.Port, err)
	}
	s.setAddr(ln.Addr().String())
	defer s.setAddr("")

	httpSrv := &http.Server{Handler: handler}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	shutdown := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}

	select {
	case <-ctx.Done():
		shutdown()
		cancel()
		<-backend.Done()
		return nil
	case err := <-serveErr:
		cancel()
		<-backend.Done()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api: http server exited: %w", err)
		}
		return nil
	case <-backend.Done():
		shutdown()
		if err := backend.Err(); err != nil {
			return fmt.Errorf("api: backend exited unexpectedly: %w", err)
		}
		return errors.New("api: backend exited unexpectedly")
	}
}

// Addr returns the server's actual bound address (e.g. "127.0.0.1:4222"),
// useful for tests/health checks. It returns "" when the Server isn't
// currently running.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.addr
}

func (s *Server) setAddr(a string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addr = a
}
