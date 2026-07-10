package management

import (
	"testing"

	"botson/internal/engine/tools"
)

// TestUpdateSettings guards the two behaviors handleSettingsSet used to
// implement inline in internal/natsapi before that logic moved here: a
// WorkspaceRoot change must apply live (tools.SetWorkspaceRoot), and only a
// ModelName/Provider change should report modelOrProviderChanged, since
// those two (unlike WorkspaceRoot) need a core restart to actually take
// effect.
func TestUpdateSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	newRoot := t.TempDir()
	patch := SettingsPatch{WorkspaceRoot: &newRoot}

	masked, changed, err := UpdateSettings(patch)
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if changed {
		t.Fatalf("expected modelOrProviderChanged=false for a WorkspaceRoot-only patch, got true")
	}
	if masked.WorkspaceRoot != newRoot {
		t.Fatalf("expected persisted WorkspaceRoot %q, got %q", newRoot, masked.WorkspaceRoot)
	}
	if got := tools.WorkspaceRoot; got != newRoot {
		t.Fatalf("expected tools.WorkspaceRoot to be updated live, got %q", got)
	}

	newModel := "gemini-3.1-pro"
	masked, changed, err = UpdateSettings(SettingsPatch{ModelName: &newModel})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if !changed {
		t.Fatalf("expected modelOrProviderChanged=true for a ModelName patch, got false")
	}
	if masked.ModelName != newModel {
		t.Fatalf("expected persisted ModelName %q, got %q", newModel, masked.ModelName)
	}
}
