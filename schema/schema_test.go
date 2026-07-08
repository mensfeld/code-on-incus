package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/mensfeld/code-on-incus/schema"
)

func mustGetSchema(t *testing.T) []byte {
	t.Helper()
	b, err := schema.GetProfileSchema()
	if err != nil {
		t.Fatalf("GetProfileSchema returned error: %v", err)
	}
	return b
}

func TestGetProfileSchema_NonEmpty(t *testing.T) {
	b := mustGetSchema(t)
	if len(b) == 0 {
		t.Fatal("GetProfileSchema returned empty bytes")
	}
}

func TestGetProfileSchema_ValidJSON(t *testing.T) {
	b := mustGetSchema(t)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("bundled profile schema is not valid JSON: %v", err)
	}
}

func TestGetProfileSchema_RequiredMetaFields(t *testing.T) {
	b := mustGetSchema(t)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	for _, key := range []string{"$schema", "$id", "title", "type", "properties", "$defs"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("bundled schema is missing top-level key %q", key)
		}
	}
}

func TestGetProfileSchema_AllDefsPresent(t *testing.T) {
	b := mustGetSchema(t)
	var doc struct {
		Defs map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	expected := []string{
		"BuildConfig", "ContainerConfig",
		"CPULimits", "MemoryLimits", "DiskLimits", "RuntimeLimits", "LimitsConfig",
		"ClaudeToolConfig", "ToolConfig",
		"MountEntry", "SocketEntry", "CredentialEntry",
		"NetworkLoggingConfig", "NetworkConfig",
		"PathsConfig", "IncusConfig",
		"GitConfig", "SSHConfig",
		"SecurityConfig",
		"NFTMonitoringConfig", "MonitoringConfig",
		"TimezoneConfig", "ShellConfig",
	}
	for _, name := range expected {
		if _, ok := doc.Defs[name]; !ok {
			t.Errorf("bundled schema is missing $defs[%q]", name)
		}
	}
}

func TestGetProfileSchema_NetworkModeEnum(t *testing.T) {
	b := mustGetSchema(t)
	var doc struct {
		Defs struct {
			NetworkConfig struct {
				Properties struct {
					Mode struct {
						Enum []any `json:"enum"`
					} `json:"mode"`
				} `json:"properties"`
			} `json:"NetworkConfig"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	modes := doc.Defs.NetworkConfig.Properties.Mode.Enum
	if len(modes) == 0 {
		t.Fatal("network mode enum is empty in bundled schema")
	}
	want := map[string]bool{"open": false, "restricted": false, "allowlist": false}
	for _, v := range modes {
		if s, ok := v.(string); ok {
			want[s] = true
		}
	}
	for mode, found := range want {
		if !found {
			t.Errorf("network mode enum missing %q in bundled schema", mode)
		}
	}
}

func TestGetProfileSchema_MountEntryRequiresHostAndContainer(t *testing.T) {
	b := mustGetSchema(t)
	var doc struct {
		Defs struct {
			MountEntry struct {
				Required []string `json:"required"`
			} `json:"MountEntry"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	required := make(map[string]bool)
	for _, f := range doc.Defs.MountEntry.Required {
		required[f] = true
	}
	for _, field := range []string{"host", "container"} {
		if !required[field] {
			t.Errorf("MountEntry.required is missing %q in bundled schema", field)
		}
	}
}

func TestGetProfileSchema_SocketEntryRequiresHostAndContainer(t *testing.T) {
	b := mustGetSchema(t)
	var doc struct {
		Defs struct {
			SocketEntry struct {
				Required             []string `json:"required"`
				AdditionalProperties bool     `json:"additionalProperties"`
				Properties           struct {
					Env map[string]any `json:"env"`
				} `json:"properties"`
			} `json:"SocketEntry"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	required := make(map[string]bool)
	for _, f := range doc.Defs.SocketEntry.Required {
		required[f] = true
	}
	for _, field := range []string{"host", "container"} {
		if !required[field] {
			t.Errorf("SocketEntry.required is missing %q in bundled schema", field)
		}
	}
	if doc.Defs.SocketEntry.AdditionalProperties {
		t.Error("SocketEntry should set additionalProperties:false to reject unknown keys")
	}
	if doc.Defs.SocketEntry.Properties.Env == nil {
		t.Error("SocketEntry should declare an optional 'env' property")
	}
}

// The profile schema must wire a top-level `sockets` array referencing SocketEntry.
func TestGetProfileSchema_SocketsArrayWired(t *testing.T) {
	b := mustGetSchema(t)
	var doc struct {
		Properties struct {
			Sockets struct {
				Type  string `json:"type"`
				Items struct {
					Ref string `json:"$ref"`
				} `json:"items"`
			} `json:"sockets"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	if doc.Properties.Sockets.Type != "array" {
		t.Errorf("profile schema 'sockets' should be an array, got %q", doc.Properties.Sockets.Type)
	}
	if doc.Properties.Sockets.Items.Ref != "#/$defs/SocketEntry" {
		t.Errorf("profile schema 'sockets' items should $ref SocketEntry, got %q", doc.Properties.Sockets.Items.Ref)
	}
}

// The profile schema must expose an `env_commands` object of string values.
func TestGetProfileSchema_EnvCommandsWired(t *testing.T) {
	b := mustGetSchema(t)
	var doc struct {
		Properties struct {
			EnvCommands struct {
				Type                 string         `json:"type"`
				AdditionalProperties map[string]any `json:"additionalProperties"`
			} `json:"env_commands"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	if doc.Properties.EnvCommands.Type != "object" {
		t.Errorf("profile schema 'env_commands' should be an object, got %q", doc.Properties.EnvCommands.Type)
	}
	if doc.Properties.EnvCommands.AdditionalProperties["type"] != "string" {
		t.Errorf("env_commands values should be strings, got %v", doc.Properties.EnvCommands.AdditionalProperties)
	}
}

// Regression: these keys are supported by the Go structs but were missing from
// the JSON schema, so `coi validate profile` rejected valid profiles that used
// them (container.stale_base_check, monitoring.process_count_threshold,
// monitoring.process_spawn_rate_threshold). ValidateProfileMap must now accept them.
func TestValidateProfileMap_AcceptsStructSupportedKeys(t *testing.T) {
	profile := map[string]any{
		"container": map[string]any{
			"stale_base_check": "error",
		},
		"monitoring": map[string]any{
			"process_count_threshold":      256,
			"process_spawn_rate_threshold": 100,
		},
	}
	if err := schema.ValidateProfileMap(profile); err != nil {
		t.Fatalf("profile with struct-supported keys should validate, got: %v", err)
	}
}

// stale_base_check is constrained to the enum the runtime understands.
func TestValidateProfileMap_RejectsBadStaleBaseCheck(t *testing.T) {
	profile := map[string]any{
		"container": map[string]any{"stale_base_check": "loud"},
	}
	if err := schema.ValidateProfileMap(profile); err == nil {
		t.Fatal("stale_base_check='loud' should be rejected (not in enum)")
	}
}

// The strict schema must still reject genuinely unknown keys.
func TestValidateProfileMap_RejectsUnknownKey(t *testing.T) {
	profile := map[string]any{
		"container": map[string]any{"bogus_key": true},
	}
	if err := schema.ValidateProfileMap(profile); err == nil {
		t.Fatal("unknown container key should be rejected by additionalProperties:false")
	}
}

func TestGetProfileSchema_Deterministic(t *testing.T) {
	a, err := schema.GetProfileSchema()
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	b, err := schema.GetProfileSchema()
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("GetProfileSchema is not deterministic across calls")
	}
}
