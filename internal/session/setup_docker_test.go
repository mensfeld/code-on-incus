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

	for _, key := range []string{"bip", "default-address-pools", "group"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("dockerDaemonJSON missing %q key", key)
		}
	}
}

func TestDockerDaemonJSON_PreservesGroupSetting(t *testing.T) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(dockerDaemonJSON), &parsed); err != nil {
		t.Fatalf("dockerDaemonJSON is not valid JSON: %v", err)
	}

	// "group": "code" must be present so the non-root `code` user retains
	// access to /var/run/docker.sock after container reboot (base-image setting).
	group, ok := parsed["group"]
	if !ok {
		t.Fatal("dockerDaemonJSON missing 'group' key — non-root Docker access will break")
	}
	if group != "code" {
		t.Errorf("dockerDaemonJSON group = %q, want %q", group, "code")
	}
}

func TestDockerDaemonJSON_BridgeCIDR(t *testing.T) {
	var parsed struct {
		BIP                 string `json:"bip"`
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
