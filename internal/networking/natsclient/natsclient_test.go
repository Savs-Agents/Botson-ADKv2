package natsclient

import "testing"

// TestConnect_UnreachableAddress confirms Connect returns a wrapped error
// rather than hanging or panicking when nothing is listening -- there's no
// in-process NATS broker left in this codebase to test a real connection
// against (the embedded server was dropped in the 2026-07 HTTP-native
// rework), so this is the extent of what's testable here today.
func TestConnect_UnreachableAddress(t *testing.T) {
	_, err := Connect("nats://127.0.0.1:1", "")
	if err == nil {
		t.Fatal("expected an error connecting to an unreachable address, got nil")
	}
}
