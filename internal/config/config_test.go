package config

import (
	"encoding/json"
	"os"
	"testing"
)

// TestUpdateMutatesSharedInstanceInPlace guards the guarantee that makes
// self-configuration mid-conversation possible: Update must mutate the
// exact struct Load already handed out, not swap in a new one, so every
// other holder of that pointer (e.g. cmd/botson-core's appBoot.Config) sees the
// change without needing to reload.
func TestUpdateMutatesSharedInstanceInPlace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetCacheForTest()

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	updated, err := Update(func(cfg *AppConfig) {
		cfg.RootAgent = "Changed Agent"
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if loaded != updated {
		t.Fatalf("Update returned a different pointer than Load; self-configuration would go unnoticed by existing holders")
	}
	if loaded.RootAgent != "Changed Agent" {
		t.Fatalf("original pointer from Load did not observe Update's change: got %q", loaded.RootAgent)
	}

	configPath, err := GetConfigPath()
	if err != nil {
		t.Fatalf("GetConfigPath failed: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file directly: %v", err)
	}
	var onDisk AppConfig
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("failed to parse on-disk config: %v", err)
	}
	if onDisk.RootAgent != "Changed Agent" {
		t.Fatalf("Update did not persist to disk: got %q", onDisk.RootAgent)
	}
}

// TestLoadBackfillsHostPortAndToken guards the defaults a fresh install
// needs for Botson's HTTP API server to have somewhere to bind and a
// credential to gate it with -- and that those defaults are persisted to
// disk immediately (like WorkspaceRoot), not just fixed up in memory.
func TestLoadBackfillsHostPortAndToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetCacheForTest()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Host != "127.0.0.1" {
		t.Fatalf("expected default Host 127.0.0.1, got %q", cfg.Host)
	}
	if cfg.Port != 4222 {
		t.Fatalf("expected default Port 4222, got %d", cfg.Port)
	}
	if cfg.ApiAuthToken == "" {
		t.Fatalf("expected a generated ApiAuthToken, got empty string")
	}

	configPath, err := GetConfigPath()
	if err != nil {
		t.Fatalf("GetConfigPath failed: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file directly: %v", err)
	}
	var onDisk AppConfig
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("failed to parse on-disk config: %v", err)
	}
	if onDisk.Host != cfg.Host || onDisk.Port != cfg.Port || onDisk.ApiAuthToken != cfg.ApiAuthToken {
		t.Fatalf("backfilled Host/Port/ApiAuthToken were not persisted to disk: got %+v", onDisk)
	}
}

// TestMaskHidesTokenButExposesHostPort guards that Mask blanks the secret
// bearer token entirely while leaving Host/Port visible -- they're meant
// to be shown back in a settings reply, unlike the token.
func TestMaskHidesTokenButExposesHostPort(t *testing.T) {
	cfg := &AppConfig{
		Host:         "127.0.0.1",
		Port:         4222,
		ApiAuthToken: "some-secret-token",
	}

	masked := Mask(cfg)

	if masked.ApiAuthToken != "" {
		t.Fatalf("expected ApiAuthToken to be blanked, got %q", masked.ApiAuthToken)
	}
	if masked.Host != cfg.Host {
		t.Fatalf("expected Host to remain visible, got %q", masked.Host)
	}
	if masked.Port != cfg.Port {
		t.Fatalf("expected Port to remain visible, got %d", masked.Port)
	}
}

// resetCacheForTest clears the package-level cache between tests so each
// test's t.TempDir()-scoped HOME gets its own fresh Load, instead of
// silently reusing whatever an earlier test cached in-process.
func resetCacheForTest() {
	mu.Lock()
	defer mu.Unlock()
	cached = nil
}
