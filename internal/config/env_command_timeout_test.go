package config

import "testing"

// A profile's env_command_timeout is applied to the resolved config, giving the
// per-profile timeout parity with the [defaults] env_command_timeout it mirrors.
func TestApplyProfile_EnvCommandTimeout(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]ProfileConfig{
			"p": {EnvCommandTimeout: "12s"},
		},
	}
	if err := cfg.ApplyProfile("p"); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if cfg.Defaults.EnvCommandTimeout != "12s" {
		t.Errorf("env_command_timeout: got %q, want 12s", cfg.Defaults.EnvCommandTimeout)
	}
}

// An unset profile timeout leaves a base config value untouched.
func TestApplyProfile_EnvCommandTimeout_UnsetKeepsBase(t *testing.T) {
	cfg := &Config{
		Defaults: DefaultsConfig{EnvCommandTimeout: "30s"},
		Profiles: map[string]ProfileConfig{"p": {}},
	}
	if err := cfg.ApplyProfile("p"); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if cfg.Defaults.EnvCommandTimeout != "30s" {
		t.Errorf("base timeout should survive an unset profile timeout, got %q", cfg.Defaults.EnvCommandTimeout)
	}
}

// Across an inheritance chain the child's timeout wins, else the parent's is
// inherited — matching the scalar-inherit convention of the other fields.
func TestMergeProfiles_EnvCommandTimeout(t *testing.T) {
	t.Run("child overrides parent", func(t *testing.T) {
		got := mergeProfiles(
			ProfileConfig{EnvCommandTimeout: "5s"},
			ProfileConfig{EnvCommandTimeout: "9s"},
		)
		if got.EnvCommandTimeout != "9s" {
			t.Errorf("got %q, want child value 9s", got.EnvCommandTimeout)
		}
	})
	t.Run("child inherits parent", func(t *testing.T) {
		got := mergeProfiles(
			ProfileConfig{EnvCommandTimeout: "5s"},
			ProfileConfig{},
		)
		if got.EnvCommandTimeout != "5s" {
			t.Errorf("got %q, want inherited parent value 5s", got.EnvCommandTimeout)
		}
	})
}

// The timeout governs env_commands, which are host code execution — so an
// untrusted (project-scoped) profile has it stripped alongside env_commands.
func TestLoadProfileDirectories_UntrustedStripsEnvCommandTimeout(t *testing.T) {
	body := "env_command_timeout = \"99s\"\n[env_commands]\nTOKEN = \"echo secret\"\n"
	root, _ := writeProfile(t, body)
	cfg := GetDefaultConfig()
	if err := loadProfileDirectories(cfg, root, false); err != nil {
		t.Fatalf("loadProfileDirectories: %v", err)
	}
	if to := cfg.Profiles["dev"].EnvCommandTimeout; to != "" {
		t.Errorf("env_command_timeout from untrusted profile must be stripped, got %q", to)
	}
}

// A LONE env_command_timeout (no env_commands) in an untrusted profile must
// still be stripped: otherwise it would survive and override the timeout applied
// to trusted-scope env_commands when this profile is selected.
func TestLoadProfileDirectories_UntrustedStripsLoneEnvCommandTimeout(t *testing.T) {
	body := "env_command_timeout = \"99s\"\n"
	root, _ := writeProfile(t, body)
	cfg := GetDefaultConfig()
	if err := loadProfileDirectories(cfg, root, false); err != nil {
		t.Fatalf("loadProfileDirectories: %v", err)
	}
	if to := cfg.Profiles["dev"].EnvCommandTimeout; to != "" {
		t.Errorf("a lone env_command_timeout from an untrusted profile must be stripped, got %q", to)
	}
}

// The same lone-timeout strip at top-level (untrusted project config) scope.
func TestSanitizeUntrustedEnvCommands_StripsLoneTimeout(t *testing.T) {
	d := &DefaultsConfig{EnvCommandTimeout: "99s"}
	sanitizeUntrustedEnvCommands(d, "/ws/.coi/config.toml")
	if d.EnvCommandTimeout != "" {
		t.Errorf("a lone env_command_timeout from untrusted config must be stripped, got %q", d.EnvCommandTimeout)
	}
}

// A trusted profile keeps both env_commands and their timeout.
func TestLoadProfileDirectories_TrustedKeepsEnvCommandTimeout(t *testing.T) {
	body := "env_command_timeout = \"99s\"\n[env_commands]\nTOKEN = \"echo secret\"\n"
	root, _ := writeProfile(t, body)
	cfg := GetDefaultConfig()
	if err := loadProfileDirectories(cfg, root, true); err != nil {
		t.Fatalf("loadProfileDirectories: %v", err)
	}
	if to := cfg.Profiles["dev"].EnvCommandTimeout; to != "99s" {
		t.Errorf("env_command_timeout from trusted profile must be kept, got %q", to)
	}
}
