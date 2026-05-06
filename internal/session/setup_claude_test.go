package session

import (
	"encoding/json"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

// TestClaudeSandboxSettings_SkipPermissionPrompt verifies that ClaudeTool's
// sandbox settings include skipDangerousModePermissionPrompt: true in the
// permissions map, both in the raw settings and after mergeJSONSettings.
func TestClaudeSandboxSettings_SkipPermissionPrompt(t *testing.T) {
	ct := tool.NewClaude()
	settings := ct.GetSandboxSettings()

	// Verify permissions map exists and has the right type
	permsRaw, ok := settings["permissions"]
	if !ok {
		t.Fatal("settings missing 'permissions' key")
	}
	perms, ok := permsRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("permissions is %T, want map[string]interface{}", permsRaw)
	}

	// Verify skipDangerousModePermissionPrompt is true
	skip, ok := perms["skipDangerousModePermissionPrompt"]
	if !ok {
		t.Fatal("permissions missing 'skipDangerousModePermissionPrompt' key")
	}
	if skip != true {
		t.Errorf("skipDangerousModePermissionPrompt = %v, want true", skip)
	}

	// Verify defaultMode is still present
	if perms["defaultMode"] != "bypassPermissions" {
		t.Errorf("defaultMode = %v, want 'bypassPermissions'", perms["defaultMode"])
	}

	// Verify mergeJSONSettings produces correct JSON with the boolean value
	result, _, err := mergeJSONSettings(nil, settings)
	if err != nil {
		t.Fatalf("mergeJSONSettings() error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse merged JSON: %v", err)
	}

	permsParsed, ok := parsed["permissions"].(map[string]interface{})
	if !ok {
		t.Fatalf("merged JSON 'permissions' is %T, want map", parsed["permissions"])
	}
	if permsParsed["skipDangerousModePermissionPrompt"] != true {
		t.Errorf("merged JSON skipDangerousModePermissionPrompt = %v, want true", permsParsed["skipDangerousModePermissionPrompt"])
	}
	if permsParsed["defaultMode"] != "bypassPermissions" {
		t.Errorf("merged JSON defaultMode = %v, want 'bypassPermissions'", permsParsed["defaultMode"])
	}
}

// TestClaudeSandboxSettings_InteractiveMode verifies that interactive mode
// does NOT inject bypass permissions or skipDangerousModePermissionPrompt.
func TestClaudeSandboxSettings_InteractiveMode(t *testing.T) {
	ct := &tool.ClaudeTool{}
	ct.SetPermissionMode("interactive")
	settings := ct.GetSandboxSettings()

	if _, ok := settings["permissions"]; ok {
		t.Error("interactive mode should not include 'permissions' key")
	}
	if _, ok := settings["allowDangerouslySkipPermissions"]; ok {
		t.Error("interactive mode should not include 'allowDangerouslySkipPermissions'")
	}
}
