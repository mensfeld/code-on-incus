package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// [container] session_name parses into ContainerConfig.SessionName.
func TestSessionName_ParsesFromTOML(t *testing.T) {
	var c Config
	if _, err := toml.Decode("[container]\nsession_name = \"myproj\"\n", &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.Container.SessionName != "myproj" {
		t.Errorf("Container.SessionName = %q, want %q", c.Container.SessionName, "myproj")
	}
}

// Merge treats session_name as a scalar override (later scope wins), and an
// empty value leaves the base untouched — so a profile can name the session
// while the global config stays silent, and vice versa.
func TestSessionName_MergeOverrides(t *testing.T) {
	base := &Config{}
	base.Container.SessionName = "base"

	base.Merge(&Config{Container: ContainerConfig{SessionName: "override"}})
	if base.Container.SessionName != "override" {
		t.Errorf("after merge with a value: SessionName = %q, want %q", base.Container.SessionName, "override")
	}

	base.Merge(&Config{Container: ContainerConfig{SessionName: ""}})
	if base.Container.SessionName != "override" {
		t.Errorf("after merge with empty: SessionName = %q, want it unchanged (%q)", base.Container.SessionName, "override")
	}
}

// An untrusted (project-scoped) config must not be able to pick the session a
// launch attaches to: the session name selects a persistent container and its
// saved state (conversation history, restored credentials), so a cloned repo
// planting one could attach itself to — or poison — another project's session.
func TestSessionName_StrippedFromUntrusted(t *testing.T) {
	c := &ContainerConfig{SessionName: "victims-session"}
	sanitizeUntrustedSessionName(c, "/repo/.coi/config.toml")
	if c.SessionName != "" {
		t.Errorf("untrusted [container] session_name must be stripped, got %q", c.SessionName)
	}

	// An empty value is a no-op (nothing to strip, no spurious warning).
	empty := &ContainerConfig{}
	sanitizeUntrustedSessionName(empty, "/repo/.coi/config.toml")
	if empty.SessionName != "" {
		t.Errorf("empty SessionName must stay empty, got %q", empty.SessionName)
	}
}

// ApplyProfile carries a profile's session_name onto the effective config —
// the intended home for the setting ("point different checkouts at one named
// session" lives naturally in a profile).
func TestSessionName_AppliedFromProfile(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles = map[string]ProfileConfig{
		"proj": {Container: ContainerConfig{SessionName: "myproj"}},
	}
	if err := cfg.ApplyProfile("proj"); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if cfg.Container.SessionName != "myproj" {
		t.Errorf("profile session_name not applied: got %q", cfg.Container.SessionName)
	}
}

// Inheritance must not smuggle a trusted parent's session_name into an
// untrusted project profile: `inherits = "<trusted profile>"` would otherwise
// attach a hostile workspace to the user's named session.
func TestSessionName_InheritanceReStripped(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles = map[string]ProfileConfig{
		// Trusted profile (source under ~/.coi would be trusted; Source left
		// empty is skipped, so give it no name to strip anyway).
		"work": {Container: ContainerConfig{SessionName: "work-session"}},
		// Untrusted project-scoped child that inherited the parent's name.
		"dev": {
			Source:    "/repo/.coi/profiles/dev/config.toml",
			Container: ContainerConfig{SessionName: "work-session"},
		},
	}
	stripUntrustedInheritedSessionNames(cfg)
	if got := cfg.Profiles["dev"].Container.SessionName; got != "" {
		t.Errorf("inherited session_name in untrusted profile must be stripped, got %q", got)
	}
	if got := cfg.Profiles["work"].Container.SessionName; got != "work-session" {
		t.Errorf("trusted/sourceless profile must keep its name, got %q", got)
	}
}

// validateSessionName rejects names that would silently hash to a different
// identity than their lookalike.
func TestSessionName_Validation(t *testing.T) {
	for _, ok := range []string{"", "proj", "my-proj.2", "A_b-c.d"} {
		if err := validateSessionName(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"proj ", " proj", "pro j", "a/b", "-lead", ".lead", "x\u200by"} {
		if err := validateSessionName(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
