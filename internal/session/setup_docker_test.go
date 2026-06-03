package session

import (
	"encoding/json"
	"testing"
)

func TestDockerDaemonJSON_ValidJSON(t *testing.T) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(dockerDaemonJSON), &parsed); err != nil {
		t.Fatalf("dockerDaemonJSON is not valid JSON: %v", err)
	}

	if _, ok := parsed["bip"]; !ok {
		t.Error("dockerDaemonJSON missing 'bip' key")
	}
	if _, ok := parsed["default-address-pools"]; !ok {
		t.Error("dockerDaemonJSON missing 'default-address-pools' key")
	}
}

func TestDockerDaemonJSON_BridgeCIDR(t *testing.T) {
	var parsed struct {
		BIP                string `json:"bip"`
		DefaultAddressPools []struct {
			Base string `json:"base"`
			Size int    `json:"size"`
		} `json:"default-address-pools"`
	}
	if err := json.Unmarshal([]byte(dockerDaemonJSON), &parsed); err != nil {
		t.Fatalf("failed to parse dockerDaemonJSON: %v", err)
	}

	if parsed.BIP == "" {
		t.Error("bip must not be empty")
	}
	if len(parsed.DefaultAddressPools) == 0 {
		t.Error("default-address-pools must have at least one entry")
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's fine", "'it'\"'\"'s fine'"},
		{`{"key":"value"}`, `'{"key":"value"}'`},
		{"line1\nline2", "'line1\nline2'"},
		{"", "''"},
	}

	for _, tt := range tests {
		got := shellEscape(tt.input)
		if got != tt.want {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
