package adkproxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// NewReverseProxy returns an http.Handler that forwards every request
// (method, path, query, body, headers) verbatim to backendBaseURL and
// relays the response back unchanged. backendBaseURL always has an empty
// path (e.g. "http://127.0.0.1:8080"), so httputil's default Director joins
// it with the incoming request path unmodified -- no path rewriting is
// needed here; the ADK backend's own routing (/api/*, /a2a/*,
// /.well-known/agent-card.json) is untouched, and the caller (see
// internal/apiserver) mounts this handler at those same paths.
func NewReverseProxy(backendBaseURL string, requestTimeout time.Duration, logger *slog.Logger) (http.Handler, error) {
	u, err := url.Parse(backendBaseURL)
	if err != nil {
		return nil, fmt.Errorf("adkproxy: parse backend URL %q: %w", backendBaseURL, err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	if requestTimeout <= 0 {
		requestTimeout = 30 * time.Second
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	// ResponseHeaderTimeout bounds how long we wait for the backend to
	// start responding, not the total transfer -- for /api/run, that's the
	// whole agent turn (see backend.go's serverWriteTimeout doc comment on
	// why real turns run for minutes, not seconds). Callers should pass a
	// value with real headroom above their longest expected turn.
	proxy.Transport = &http.Transport{ResponseHeaderTimeout: requestTimeout}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("adkproxy: backend request failed", "path", r.URL.Path, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	}
	return proxy, nil
}
