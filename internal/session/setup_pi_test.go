package session

import (
	"encoding/json"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

// TestBuildJSONFromSettings_PiDefault verifies that pi's sandbox
// settings (default, no context path) produce valid empty JSON.
func TestBuildJSONFromSettings_PiDefault(t *testing.T) {
	pi := tool.NewPi()
	settings := pi.GetSandboxSettings()

	// Pi with no context path returns empty settings
	if len(settings) != 0 {
		jsonStr, err := buildJSONFromSettings(settings)
		if err != nil {
			t.Fatalf("buildJSONFromSettings() error: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			t.Fatalf("Failed to parse JSON output: %v", err)
		}
	}
}

// TestBuildJSONFromSettings_PiWithContext verifies that pi's sandbox
// settings are empty even when context path is set.
// Context injection is handled via PreLaunch (symlinks APPEND_SYSTEM.md), not settings.json.
func TestBuildJSONFromSettings_PiWithContext(t *testing.T) {
	pi := &tool.PiTool{}
	pi.SetAutoContextPath("/home/code/SANDBOX_CONTEXT.md")
	settings := pi.GetSandboxSettings()

	// Context injection is handled via PreLaunch (symlinks APPEND_SYSTEM.md), not settings.json
	if len(settings) != 0 {
		t.Errorf("GetSandboxSettings() = %v, want empty map", settings)
	}
}

// TestBuildJSONFromSettings_PiRoundTrip verifies that the JSON output
// can round-trip: marshal -> unmarshal -> re-marshal produces identical output.
func TestBuildJSONFromSettings_PiRoundTrip(t *testing.T) {
	pi := &tool.PiTool{}
	pi.SetAutoContextPath("/home/code/SANDBOX_CONTEXT.md")
	settings := pi.GetSandboxSettings()

	jsonStr, err := buildJSONFromSettings(settings)
	if err != nil {
		t.Fatalf("buildJSONFromSettings() error: %v", err)
	}

	// Unmarshal and re-marshal
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse first JSON: %v", err)
	}

	jsonStr2, err := buildJSONFromSettings(parsed)
	if err != nil {
		t.Fatalf("second buildJSONFromSettings() error: %v", err)
	}

	if jsonStr != jsonStr2 {
		t.Errorf("Round-trip mismatch:\n  first:  %s\n  second: %s", jsonStr, jsonStr2)
	}
}
