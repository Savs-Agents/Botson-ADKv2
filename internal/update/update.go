// Package update will implement `botson update`: checking for and applying
// a newer release of the binary in place. Not implemented yet -- this is
// just the framework (package + wired-up command) for that to land in
// later, same shape as internal/daemon and internal/management so
// cmd/botson-core stays a thin wrapper.
package update

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by Run until the actual update logic lands.
var ErrNotImplemented = errors.New("update: not implemented yet")

// Run will check for and apply an update. It's a stub today.
func Run(ctx context.Context) error {
	return ErrNotImplemented
}
