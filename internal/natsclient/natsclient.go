// Package natsclient is scaffolding for a possible future application-level
// NATS feature (e.g. publishing agent-run events for external subscribers
// to consume). It is deliberately not wired into anything today -- nothing
// in cmd/botson-core calls Connect, and no code publishes or subscribes to
// any subject. Botson's actual API (internal/apiserver) is plain HTTP; this
// package exists purely so the nats.go client dependency (kept on purpose
// -- see go.mod) has one obvious, documented home instead of being an
// unexplained stray import if/when it's actually used.
package natsclient

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Connect dials a NATS server at url (e.g. "nats://127.0.0.1:4222"),
// optionally authenticating with token if non-empty. Returns a ready
// *nats.Conn; callers are responsible for closing it.
func Connect(url, token string) (*nats.Conn, error) {
	opts := []nats.Option{nats.Timeout(5 * time.Second)}
	if token != "" {
		opts = append(opts, nats.Token(token))
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("natsclient: connect to %s: %w", url, err)
	}
	return nc, nil
}
