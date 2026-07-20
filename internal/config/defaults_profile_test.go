package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// [defaults] profile parses into DefaultsConfig.Profile (#607).
func TestDefaultsProfile_ParsesFromTOML(t *testing.T) {
	var c Config
	if _, err := toml.Decode("[defaults]\nprofile = \"pickles\"\n", &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.Defaults.Profile != "pickles" {
		t.Errorf("Defaults.Profile = %q, want %q", c.Defaults.Profile, "pickles")
	}
}

// Merge treats [defaults] profile as a scalar override (later scope wins), like
// model — a project/profile layer with a value replaces the base, and an empty
// value leaves the base untouched.
func TestDefaultsProfile_MergeOverrides(t *testing.T) {
	base := &Config{}
	base.Defaults.Profile = "base"

	base.Merge(&Config{Defaults: DefaultsConfig{Profile: "override"}})
	if base.Defaults.Profile != "override" {
		t.Errorf("after merge with a value: Profile = %q, want %q", base.Defaults.Profile, "override")
	}

	base.Merge(&Config{Defaults: DefaultsConfig{Profile: ""}})
	if base.Defaults.Profile != "override" {
		t.Errorf("after merge with empty: Profile = %q, want it unchanged (%q)", base.Defaults.Profile, "override")
	}
}

// An untrusted (project-scoped) config must not be able to redirect the no-flag
// default profile — the field is stripped before merge so a cloned repo cannot
// downgrade the user's environment.
func TestDefaultsProfile_StrippedFromUntrusted(t *testing.T) {
	d := &DefaultsConfig{Profile: "attacker"}
	sanitizeUntrustedDefaultProfile(d, "/repo/.coi/config.toml")
	if d.Profile != "" {
		t.Errorf("untrusted [defaults] profile must be stripped, got %q", d.Profile)
	}

	// An empty value is a no-op (nothing to strip, no spurious warning).
	empty := &DefaultsConfig{}
	sanitizeUntrustedDefaultProfile(empty, "/repo/.coi/config.toml")
	if empty.Profile != "" {
		t.Errorf("empty Profile must stay empty, got %q", empty.Profile)
	}
}
