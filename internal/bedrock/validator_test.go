package bedrock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsBedrockConfigured(t *testing.T) {
	tests := []struct {
		name           string
		settingsJSON   string
		expectConfiged bool
		expectError    bool
	}{
		{
			name: "Bedrock configured",
			settingsJSON: `{
				"anthropic": {
					"apiProvider": "bedrock",
					"bedrock": {
						"region": "us-west-2"
					}
				}
			}`,
			expectConfiged: true,
			expectError:    false,
		},
		{
			name: "Bedrock not configured",
			settingsJSON: `{
				"anthropic": {
					"apiKey": "test"
				}
			}`,
			expectConfiged: false,
			expectError:    false,
		},
		{
			name:           "File doesn't exist",
			settingsJSON:   "",
			expectConfiged: false,
			expectError:    false,
		},
		{
			name:           "Invalid JSON",
			settingsJSON:   `{invalid}`,
			expectConfiged: false,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			settingsPath := filepath.Join(tmpDir, "settings.json")

			if tt.settingsJSON != "" {
				if err := os.WriteFile(settingsPath, []byte(tt.settingsJSON), 0o644); err != nil {
					t.Fatalf("Failed to write test file: %v", err)
				}
			}

			configured, err := IsBedrockConfigured(settingsPath)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if configured != tt.expectConfiged {
				t.Errorf("Expected configured=%v, got %v", tt.expectConfiged, configured)
			}
		})
	}
}

func TestValidationResult_HasErrors(t *testing.T) {
	tests := []struct {
		name   string
		result *ValidationResult
		expect bool
	}{
		{
			name: "Has errors",
			result: &ValidationResult{
				Issues: []ValidationIssue{
					{Severity: "error", Message: "test"},
				},
			},
			expect: true,
		},
		{
			name: "Only warnings",
			result: &ValidationResult{
				Issues: []ValidationIssue{
					{Severity: "warning", Message: "test"},
				},
			},
			expect: false,
		},
		{
			name: "No issues",
			result: &ValidationResult{
				Issues: []ValidationIssue{},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasErrors(); got != tt.expect {
				t.Errorf("Expected HasErrors()=%v, got %v", tt.expect, got)
			}
		})
	}
}

func TestValidationResult_FormatError(t *testing.T) {
	result := &ValidationResult{
		Issues: []ValidationIssue{
			{
				Severity: "error",
				Message:  "Test error",
				Fix:      "Fix it",
			},
			{
				Severity: "warning",
				Message:  "Test warning",
				Fix:      "",
			},
		},
	}

	formatted := result.FormatError()

	// Check that output contains expected elements
	expectedElements := []string{
		"AWS Bedrock configuration detected",
		"❌",
		"⚠️",
		"Test error",
		"Fix: Fix it",
		"Test warning",
	}

	for _, elem := range expectedElements {
		if !contains(formatted, elem) {
			t.Errorf("Expected formatted output to contain %q, got:\n%s", elem, formatted)
		}
	}
}

func TestCheckMountConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		mounts      []string
		expectIssue bool
	}{
		{
			name:        ".aws mounted",
			mounts:      []string{"/home/user/.aws", "/workspace"},
			expectIssue: false,
		},
		{
			name:        ".aws not mounted",
			mounts:      []string{"/workspace"},
			expectIssue: true,
		},
		{
			name:        "No mounts",
			mounts:      []string{},
			expectIssue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := CheckMountConfiguration(tt.mounts)
			hasIssue := issue != nil

			if hasIssue != tt.expectIssue {
				t.Errorf("Expected issue=%v, got %v (issue: %v)", tt.expectIssue, hasIssue, issue)
			}

			if hasIssue && issue.Severity != "error" {
				t.Errorf("Expected error severity, got %s", issue.Severity)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSettingsJSONStructure(t *testing.T) {
	// Test that we can properly parse real-world settings.json structure
	realWorldJSON := `{
		"anthropic": {
			"apiProvider": "bedrock",
			"bedrock": {
				"region": "us-west-2",
				"profile": "default"
			}
		},
		"other": {
			"setting": "value"
		}
	}`

	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "settings.json")

	if err := os.WriteFile(settingsPath, []byte(realWorldJSON), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	configured, err := IsBedrockConfigured(settingsPath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !configured {
		t.Error("Expected Bedrock to be configured")
	}

	// Verify we can parse it back
	var settings map[string]interface{}
	data, _ := os.ReadFile(settingsPath)
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	anthropic := settings["anthropic"].(map[string]interface{})
	if anthropic["apiProvider"] != "bedrock" {
		t.Error("Expected apiProvider to be bedrock")
	}
}
