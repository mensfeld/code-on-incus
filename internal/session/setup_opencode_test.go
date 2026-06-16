package session

import (
	"encoding/json"
	"strings"
	"testing"
)

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
