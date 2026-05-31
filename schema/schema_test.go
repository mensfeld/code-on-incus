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
		"MountEntry",
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

func TestProfileSchemaID(t *testing.T) {
	id, err := schema.ProfileSchemaID()
	if err != nil {
		t.Fatalf("ProfileSchemaID returned error: %v", err)
	}
	if id == "" {
		t.Fatal("ProfileSchemaID returned empty string")
	}
}
