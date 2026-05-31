package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/mensfeld/code-on-incus/schema"
)

func TestGetProfileSchema_NonEmpty(t *testing.T) {
	b := schema.GetProfileSchema()
	if len(b) == 0 {
		t.Fatal("GetProfileSchema returned empty bytes")
	}
}

func TestGetProfileSchema_ValidJSON(t *testing.T) {
	b := schema.GetProfileSchema()
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("profile schema is not valid JSON: %v", err)
	}
}

func TestGetProfileSchema_RequiredFields(t *testing.T) {
	b := schema.GetProfileSchema()
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	for _, key := range []string{"$schema", "$id", "title", "type", "properties", "$defs"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("schema is missing top-level key %q", key)
		}
	}
}

func TestGetProfileSchema_NetworkModeEnum(t *testing.T) {
	b := schema.GetProfileSchema()
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
		t.Fatal("network mode enum is empty")
	}

	want := map[string]bool{"open": false, "restricted": false, "allowlist": false}
	for _, v := range modes {
		if s, ok := v.(string); ok {
			want[s] = true
		}
	}
	for mode, found := range want {
		if !found {
			t.Errorf("network mode enum missing %q", mode)
		}
	}
}

func TestGetProfileSchema_MountEntryRequiresHostAndContainer(t *testing.T) {
	b := schema.GetProfileSchema()
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
			t.Errorf("MountEntry.required is missing %q", field)
		}
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
