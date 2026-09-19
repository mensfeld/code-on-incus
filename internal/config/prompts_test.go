package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

// PromptEntry accepts both the inline-string and { file = "..." } table forms,
// and rejects any other shape loudly at decode time.
func TestPromptEntry_UnmarshalTOML(t *testing.T) {
	t.Run("inline string", func(t *testing.T) {
		const in = `
[prompts]
quick = "say hello"
`
		var cfg Config
		if _, err := toml.Decode(in, &cfg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		got := cfg.Prompts["quick"]
		if got.Text != "say hello" || got.File != "" {
			t.Fatalf("got %+v, want Text=\"say hello\" File=\"\"", got)
		}
	})

	t.Run("file table", func(t *testing.T) {
		const in = `
[prompts]
triage = { file = "prompts/triage.md" }
`
		var cfg Config
		if _, err := toml.Decode(in, &cfg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		got := cfg.Prompts["triage"]
		if got.File != "prompts/triage.md" || got.Text != "" {
			t.Fatalf("got %+v, want File=\"prompts/triage.md\" Text=\"\"", got)
		}
	})

	t.Run("table without file key is rejected", func(t *testing.T) {
		const in = `
[prompts]
bad = { path = "x" }
`
		var cfg Config
		if _, err := toml.Decode(in, &cfg); err == nil {
			t.Fatal("expected error for table without a 'file' key")
		}
	})

	t.Run("non-string file value is rejected", func(t *testing.T) {
		const in = `
[prompts]
bad = { file = 3 }
`
		var cfg Config
		if _, err := toml.Decode(in, &cfg); err == nil {
			t.Fatal("expected error for non-string 'file' value")
		}
	})

	t.Run("table with unknown key is rejected", func(t *testing.T) {
		const in = `
[prompts]
bad = { file = "a.md", fille = "b.md" }
`
		var cfg Config
		if _, err := toml.Decode(in, &cfg); err == nil {
			t.Fatal("expected error for a table with an unknown key (typo should not be silently ignored)")
		}
	})
}

// ResolvePrompt returns inline text, reads file entries, and errors clearly on a
// missing name or an empty resolution.
func TestConfig_ResolvePrompt(t *testing.T) {
	dir := t.TempDir()
	promptFile := filepath.Join(dir, "triage.md")
	if err := os.WriteFile(promptFile, []byte("Triage the open issues.\n"), 0o600); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	emptyFile := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(emptyFile, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	cfg := &Config{Prompts: map[string]PromptEntry{
		"quick":  {Text: "say hello"},
		"triage": {File: promptFile},
		"blank":  {Text: "   "},
		"efile":  {File: emptyFile},
	}}

	t.Run("inline", func(t *testing.T) {
		got, err := cfg.ResolvePrompt("quick")
		if err != nil || got != "say hello" {
			t.Fatalf("got (%q, %v), want (\"say hello\", nil)", got, err)
		}
	})

	t.Run("file", func(t *testing.T) {
		got, err := cfg.ResolvePrompt("triage")
		if err != nil || got != "Triage the open issues.\n" {
			t.Fatalf("got (%q, %v)", got, err)
		}
	})

	t.Run("missing name lists available", func(t *testing.T) {
		_, err := cfg.ResolvePrompt("nope")
		if err == nil {
			t.Fatal("expected error for missing name")
		}
	})

	t.Run("empty inline", func(t *testing.T) {
		if _, err := cfg.ResolvePrompt("blank"); err == nil {
			t.Fatal("expected error for empty inline prompt")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		if _, err := cfg.ResolvePrompt("efile"); err == nil {
			t.Fatal("expected error for empty file prompt")
		}
	})

	t.Run("missing name with no prompts", func(t *testing.T) {
		empty := &Config{}
		if _, err := empty.ResolvePrompt("x"); err == nil {
			t.Fatal("expected error when no prompts configured")
		}
	})
}

// resolvePromptFiles rewrites relative file= paths against the config dir,
// leaves absolute paths and inline text untouched.
func TestResolvePromptFiles(t *testing.T) {
	prompts := map[string]PromptEntry{
		"inline": {Text: "keep me"},
		"rel":    {File: "prompts/x.md"},
		"abs":    {File: "/abs/y.md"},
	}
	resolvePromptFiles(prompts, "/base/dir")

	if got := prompts["inline"]; got.Text != "keep me" || got.File != "" {
		t.Errorf("inline entry changed: %+v", got)
	}
	if got := prompts["rel"].File; got != filepath.Join("/base/dir", "prompts/x.md") {
		t.Errorf("rel: got %q", got)
	}
	if got := prompts["abs"].File; got != "/abs/y.md" {
		t.Errorf("abs path should pass through: got %q", got)
	}
}

// An untrusted project config's [prompts] are stripped WHOLESALE — inline text
// and file= alike — so a named prompt is honored only from trusted scope.
func TestSanitizeUntrustedPrompts(t *testing.T) {
	const projectTOML = `
[prompts]
inline = "inline redefinition"
leak = { file = "~/.ssh/id_rsa" }
`
	var cfg Config
	if _, err := toml.Decode(projectTOML, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sanitizeUntrustedConfig(&cfg, "/ws/.coi/config.toml")

	if len(cfg.Prompts) != 0 {
		t.Errorf("all prompts from untrusted config should be stripped, got %+v", cfg.Prompts)
	}
}

// Config.Merge layers prompt maps last-wins.
func TestConfigMerge_Prompts(t *testing.T) {
	base := &Config{Prompts: map[string]PromptEntry{
		"a": {Text: "base-a"},
		"b": {Text: "base-b"},
	}}
	other := &Config{Prompts: map[string]PromptEntry{
		"b": {Text: "other-b"},
		"c": {Text: "other-c"},
	}}
	base.Merge(other)

	if base.Prompts["a"].Text != "base-a" {
		t.Errorf("a: got %q, want base-a", base.Prompts["a"].Text)
	}
	if base.Prompts["b"].Text != "other-b" {
		t.Errorf("b (override): got %q, want other-b", base.Prompts["b"].Text)
	}
	if base.Prompts["c"].Text != "other-c" {
		t.Errorf("c: got %q, want other-c", base.Prompts["c"].Text)
	}
}

// mergeProfiles composes prompt maps across an inheritance chain: parent keys are
// preserved, child keys override, and a child clear-entry removes the key.
func TestMergeProfiles_Prompts(t *testing.T) {
	parent := ProfileConfig{Prompts: map[string]PromptEntry{
		"a": {Text: "parent-a"},
		"b": {Text: "parent-b"},
	}}
	child := ProfileConfig{Prompts: map[string]PromptEntry{
		"b": {Text: "child-b"},
		"a": {}, // clear inherited "a"
		"c": {Text: "child-c"},
	}}
	got := mergeProfiles(parent, child)

	if _, ok := got.Prompts["a"]; ok {
		t.Error("child empty entry should clear inherited 'a'")
	}
	if got.Prompts["b"].Text != "child-b" {
		t.Errorf("b: got %q, want child-b", got.Prompts["b"].Text)
	}
	if got.Prompts["c"].Text != "child-c" {
		t.Errorf("c: got %q, want child-c", got.Prompts["c"].Text)
	}
}

// ApplyProfile layers a profile's prompts onto the base Config.
func TestApplyProfile_Prompts(t *testing.T) {
	cfg := &Config{
		Prompts: map[string]PromptEntry{"global": {Text: "g"}},
		Profiles: map[string]ProfileConfig{
			"p": {Prompts: map[string]PromptEntry{"fromprofile": {Text: "fp"}}},
		},
	}
	if err := cfg.ApplyProfile("p"); err != nil {
		t.Fatalf("ApplyProfile: %v", err)
	}
	if cfg.Prompts["global"].Text != "g" {
		t.Error("global prompt should survive ApplyProfile")
	}
	if cfg.Prompts["fromprofile"].Text != "fp" {
		t.Error("profile prompt should be applied")
	}
}
