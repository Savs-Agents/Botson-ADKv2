package apiserver

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// authMiddleware wraps next so every request must carry a matching
// "Authorization: Bearer <token>" header, comparing the presented token to
// the configured one in constant time -- this is the one genuinely new
// security-sensitive piece of the HTTP rework: a real network-facing
// endpoint now needs the kind of per-request care NATS's own
// connection-level auth used to provide for free.
func authMiddleware(token string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("Authorization")
			presented, ok := strings.CutPrefix(got, bearerPrefix)
			if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				logger.Warn("apiserver: rejected unauthenticated request", "path", r.URL.Path, "method", r.Method)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
