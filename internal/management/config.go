package management

import (
	"fmt"

	"botson/internal/config"
	"botson/internal/engine/tools"
)

// GetMaskedConfig loads the application config and masks secret fields
// (the Gemini API key) so it's safe to hand to a UI.
func GetMaskedConfig() (*config.AppConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	masked := config.Mask(cfg)
	return &masked, nil
}

// SettingsPatch changes only the fields that are non-nil, mirroring the
// API's "only touch the fields you actually send" semantics.
type SettingsPatch struct {
	ModelName        *string
	RootAgent        *string
	GeminiAPIKey     *string
	WorkspaceRoot    *string
	Provider         *string
	OpenRouterAPIKey *string
}

// UpdateSettings applies patch to the shared config and persists it. If
// WorkspaceRoot changed, it also repoints the workspace-touching tools at
// the new root (tools.SetWorkspaceRoot) immediately, since
// tools.WorkspaceRoot is a package-level var set once at boot and would
// otherwise not pick up the change until the core restarts. The returned
// modelOrProviderChanged flag tells the caller whether to warn that this
// already-running process is still using the model/provider it booted
// with -- a change to either only takes effect on the next restart.
func UpdateSettings(patch SettingsPatch) (masked config.AppConfig, modelOrProviderChanged bool, err error) {
	modelOrProviderChanged = patch.ModelName != nil || patch.Provider != nil

	cfg, err := config.Update(func(cfg *config.AppConfig) {
		if patch.ModelName != nil {
			cfg.ModelName = *patch.ModelName
		}
		if patch.RootAgent != nil {
			cfg.RootAgent = *patch.RootAgent
		}
		if patch.GeminiAPIKey != nil {
			cfg.GeminiAPIKey = *patch.GeminiAPIKey
		}
		if patch.WorkspaceRoot != nil {
			cfg.WorkspaceRoot = *patch.WorkspaceRoot
		}
		if patch.Provider != nil {
			cfg.Provider = *patch.Provider
		}
		if patch.OpenRouterAPIKey != nil {
			cfg.OpenRouterAPIKey = *patch.OpenRouterAPIKey
		}
	})
	if err != nil {
		return config.AppConfig{}, false, err
	}

	tools.SetWorkspaceRoot(cfg.WorkspaceRoot)

	return config.Mask(cfg), modelOrProviderChanged, nil
}
