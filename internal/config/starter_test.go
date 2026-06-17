package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// The starter template (written by `coi profile create default`) must be valid
// TOML that decodes into Config without unknown keys / type errors.
func TestStarterConfigIsValidTOML(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(string(EmbeddedStarterConfig), &cfg); err != nil {
		t.Fatalf("starter template is not valid Config TOML: %v", err)
	}
}

// Anti-drift: any ACTIVE (uncommented) value in the starter must equal the
// corresponding default. Commented lines are documentation and decode to
// nothing, so the starter overrides nothing by default; this guards against a
// future active value silently diverging from the real default.
func TestStarterConfigActiveValuesMatchDefaults(t *testing.T) {
	starter := map[string]any{}
	if _, err := toml.Decode(string(EmbeddedStarterConfig), &starter); err != nil {
		t.Fatalf("decode starter: %v", err)
	}
	defaults := map[string]any{}
	if _, err := toml.Decode(string(EmbeddedDefaultConfig), &defaults); err != nil {
		t.Fatalf("decode defaults: %v", err)
	}
	assertSubset(t, "", starter, defaults)
}

// assertSubset walks every leaf present in got and asserts want has the same
// value at the same path. Empty sub-tables (active section headers with all keys
// commented) contribute no leaves and are fine.
func assertSubset(t *testing.T, path string, got, want map[string]any) {
	t.Helper()
	for k, gv := range got {
		p := k
		if path != "" {
			p = path + "." + k
		}
		wv, ok := want[k]
		if !ok {
			t.Errorf("starter sets %q but it is not a default key (drift or typo)", p)
			continue
		}
		if gm, isMap := gv.(map[string]any); isMap {
			wm, isWantMap := wv.(map[string]any)
			if !isWantMap {
				t.Errorf("%q: starter has a table, default is a scalar", p)
				continue
			}
			assertSubset(t, p, gm, wm)
			continue
		}
		if !reflect.DeepEqual(gv, wv) {
			t.Errorf("starter %q = %v, but default is %v (active starter value must match the default)", p, gv, wv)
		}
	}
}

// The starter must NOT activate secret/host-specific fields — those appear only
// as commented examples.
func TestStarterConfigHasNoActiveSecretsOrMounts(t *testing.T) {
	m := map[string]any{}
	if _, err := toml.Decode(string(EmbeddedStarterConfig), &m); err != nil {
		t.Fatalf("decode starter: %v", err)
	}
	if _, ok := m["mounts"]; ok {
		t.Error("starter must not contain an active [mounts] table (only a commented example)")
	}
	if d, ok := m["defaults"].(map[string]any); ok {
		for _, k := range []string{"forward_env", "environment"} {
			if _, bad := d[k]; bad {
				t.Errorf("starter must not activate defaults.%s (only a commented example)", k)
			}
		}
	}
}

// Every top-level config section should be represented in the starter (commented
// or not), so a newly added section forces a starter update.
func TestStarterConfigCoversAllSections(t *testing.T) {
	text := string(EmbeddedStarterConfig)
	sections := []string{
		"[container]", "[defaults]", "[paths]", "[incus]", "[network]",
		"[tool]", "[git]", "[ssh]", "[security]", "[limits.cpu]",
		"[limits.memory]", "[limits.disk]", "[limits.runtime]",
		"[monitoring]", "[shell]", "[timezone]", "[detection]",
	}
	for _, s := range sections {
		if !strings.Contains(text, s) {
			t.Errorf("starter template is missing section %s", s)
		}
	}
}

// The commented default values shown inline must match the real current
// defaults, so the documentation can't go stale silently.
func TestStarterConfigInlineDefaultsAreCurrent(t *testing.T) {
	def := GetDefaultConfig()
	text := string(EmbeddedStarterConfig)
	checks := []string{
		fmt.Sprintf("image = %q", def.Container.Image),
		fmt.Sprintf("model = %q", def.Defaults.Model),
		fmt.Sprintf("mode = %q", string(def.Network.Mode)),
		fmt.Sprintf("enforce = %q", def.Limits.Memory.Enforce),
		fmt.Sprintf("mode = %q", def.Timezone.Mode),
	}
	for _, c := range checks {
		if !strings.Contains(text, c) {
			t.Errorf("starter template missing current default line containing %q", c)
		}
	}
}
