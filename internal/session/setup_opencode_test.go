package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

// TestBuildJSONFromSettings_OpencodeBypass verifies that opencode's sandbox
// settings (bypass mode) are correctly serialized to JSON.
func TestBuildJSONFromSettings_OpencodeBypass(t *testing.T) {
	oc := tool.NewOpencode()
	settings := oc.GetSandboxSettings()

	jsonStr, err := buildJSONFromSettings(settings)
	if err != nil {
		t.Fatalf("buildJSONFromSettings() error: %v", err)
	}

	// Parse the JSON back and verify structure
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify permission key
	perm, ok := parsed["permission"]
	if !ok {
		t.Fatal("JSON missing 'permission' key")
	}
	permMap, ok := perm.(map[string]interface{})
	if !ok {
		t.Fatalf("'permission' is %T, want map", perm)
	}
	if permMap["*"] != "allow" {
		t.Errorf("permission['*'] = %v, want 'allow'", permMap["*"])
	}
}

// TestBuildJSONFromSettings_OpencodeInteractive verifies that interactive mode
// produces correct sandbox settings JSON.
func TestBuildJSONFromSettings_OpencodeInteractive(t *testing.T) {
	oc := &tool.OpencodeTool{}
	oc.SetPermissionMode("interactive")
	settings := oc.GetSandboxSettings()

	jsonStr, err := buildJSONFromSettings(settings)
	if err != nil {
		t.Fatalf("buildJSONFromSettings() error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify permission is "ask" in interactive mode
	permMap := parsed["permission"].(map[string]interface{})
	if permMap["*"] != "ask" {
		t.Errorf("permission['*'] = %v, want 'ask'", permMap["*"])
	}
}

// TestBuildJSONFromSettings_OpencodeRoundTrip verifies that the JSON output
// can round-trip: marshal -> unmarshal -> re-marshal produces identical output.
// This ensures no data is lost when the settings are written to opencode.json.
func TestBuildJSONFromSettings_OpencodeRoundTrip(t *testing.T) {
	oc := tool.NewOpencode()
	settings := oc.GetSandboxSettings()

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

// TestMergeJSONSettings_EmptyExisting verifies merging into nil/empty content
func TestMergeJSONSettings_EmptyExisting(t *testing.T) {
	settings := map[string]interface{}{
		"permission": map[string]interface{}{"*": "allow"},
	}

	result, parseErr, err := mergeJSONSettings(nil, settings)
	if err != nil {
		t.Fatalf("mergeJSONSettings() error: %v", err)
	}
	if parseErr != nil {
		t.Errorf("unexpected parseErr: %v", parseErr)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	perm, ok := parsed["permission"].(map[string]interface{})
	if !ok {
		t.Fatalf("'permission' is %T, want map[string]interface{}", parsed["permission"])
	}
	if perm["*"] != "allow" {
		t.Errorf("permission['*'] = %v, want 'allow'", perm["*"])
	}

	if !strings.HasSuffix(string(result), "\n") {
		t.Error("Result should end with trailing newline")
	}
}

// TestMergeJSONSettings_DeepMerge verifies one-level deep merge semantics
func TestMergeJSONSettings_DeepMerge(t *testing.T) {
	existing := []byte(`{
  "theme": "dark",
  "env": {
    "AWS_PROFILE": "bedrock-users",
    "AWS_REGION": "us-west-2"
  },
  "userSetting": "preserved"
}`)

	settings := map[string]interface{}{
		"env": map[string]interface{}{
			"CUSTOM_VAR": "injected",
		},
		"permission": map[string]interface{}{"*": "allow"},
	}

	result, parseErr, err := mergeJSONSettings(existing, settings)
	if err != nil {
		t.Fatalf("mergeJSONSettings() error: %v", err)
	}
	if parseErr != nil {
		t.Errorf("unexpected parseErr: %v", parseErr)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// User settings preserved
	if parsed["theme"] != "dark" {
		t.Errorf("theme = %v, want 'dark'", parsed["theme"])
	}
	if parsed["userSetting"] != "preserved" {
		t.Errorf("userSetting = %v, want 'preserved'", parsed["userSetting"])
	}

	// Env merged (not overwritten)
	env, ok := parsed["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("'env' is %T, want map[string]interface{}", parsed["env"])
	}
	if env["AWS_PROFILE"] != "bedrock-users" {
		t.Errorf("env.AWS_PROFILE = %v, want 'bedrock-users'", env["AWS_PROFILE"])
	}
	if env["CUSTOM_VAR"] != "injected" {
		t.Errorf("env.CUSTOM_VAR = %v, want 'injected'", env["CUSTOM_VAR"])
	}

	// New top-level key added
	perm, ok := parsed["permission"].(map[string]interface{})
	if !ok {
		t.Fatalf("'permission' is %T, want map[string]interface{}", parsed["permission"])
	}
	if perm["*"] != "allow" {
		t.Errorf("permission['*'] = %v, want 'allow'", perm["*"])
	}
}

// TestMergeJSONSettings_InvalidJSON verifies graceful handling of invalid JSON
func TestMergeJSONSettings_InvalidJSON(t *testing.T) {
	existing := []byte(`{invalid json with comments // not valid}`)

	settings := map[string]interface{}{
		"permission": map[string]interface{}{"*": "allow"},
	}

	result, parseErr, err := mergeJSONSettings(existing, settings)
	if err != nil {
		t.Fatalf("mergeJSONSettings() should not return err on invalid JSON, got: %v", err)
	}
	if parseErr == nil {
		t.Error("parseErr should be non-nil for invalid JSON input")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// Should contain only the settings since existing was invalid
	perm, ok := parsed["permission"].(map[string]interface{})
	if !ok {
		t.Fatalf("'permission' is %T, want map[string]interface{}", parsed["permission"])
	}
	if perm["*"] != "allow" {
		t.Errorf("permission['*'] = %v, want 'allow'", perm["*"])
	}
}

// TestMergeJSONSettings_ScalarOverwrite verifies non-map values are overwritten
func TestMergeJSONSettings_ScalarOverwrite(t *testing.T) {
	existing := []byte(`{"allowDangerouslySkipPermissions": false, "userKey": "kept"}`)

	settings := map[string]interface{}{
		"allowDangerouslySkipPermissions": true,
	}

	result, parseErr, err := mergeJSONSettings(existing, settings)
	if err != nil {
		t.Fatalf("mergeJSONSettings() error: %v", err)
	}
	if parseErr != nil {
		t.Errorf("unexpected parseErr: %v", parseErr)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if parsed["allowDangerouslySkipPermissions"] != true {
		t.Errorf("allowDangerouslySkipPermissions = %v, want true", parsed["allowDangerouslySkipPermissions"])
	}
	if parsed["userKey"] != "kept" {
		t.Errorf("userKey = %v, want 'kept'", parsed["userKey"])
	}
}

// TestMergeJSONSettings_TypedMapMerge verifies that map[string]string in settings
// (as returned by ClaudeTool.GetSandboxSettings for "env") is properly merged
// with existing map[string]interface{} from parsed JSON, not overwritten.
func TestMergeJSONSettings_TypedMapMerge(t *testing.T) {
	existing := []byte(`{
  "env": {
    "AWS_PROFILE": "bedrock-users",
    "AWS_REGION": "us-west-2"
  }
}`)

	// Simulate ClaudeTool.GetSandboxSettings() which uses map[string]string for env
	settings := map[string]interface{}{
		"env": map[string]string{
			"CLAUDE_CODE_EFFORT_LEVEL": "medium",
		},
	}

	result, _, err := mergeJSONSettings(existing, settings)
	if err != nil {
		t.Fatalf("mergeJSONSettings() error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	env, ok := parsed["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("'env' is %T, want map[string]interface{}", parsed["env"])
	}

	// User env vars preserved
	if env["AWS_PROFILE"] != "bedrock-users" {
		t.Errorf("env.AWS_PROFILE = %v, want 'bedrock-users'", env["AWS_PROFILE"])
	}
	if env["AWS_REGION"] != "us-west-2" {
		t.Errorf("env.AWS_REGION = %v, want 'us-west-2'", env["AWS_REGION"])
	}

	// Sandbox env var added
	if env["CLAUDE_CODE_EFFORT_LEVEL"] != "medium" {
		t.Errorf("env.CLAUDE_CODE_EFFORT_LEVEL = %v, want 'medium'", env["CLAUDE_CODE_EFFORT_LEVEL"])
	}
}
