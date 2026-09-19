package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// strip_attribution defaults ON: clean commit history is the out-of-the-box
// behavior (#788), like the other zero-config protections.
func TestGitStripAttribution_Defaults(t *testing.T) {
	cfg := GetDefaultConfig()
	if !cfg.Git.IsStripAttributionEnabled() {
		t.Error("strip_attribution should default to enabled")
	}
	var nilCfg *GitConfig
	if !nilCfg.IsStripAttributionEnabled() {
		t.Error("nil receiver should default to enabled")
	}
}

func TestGitStripAttribution_TOMLParse(t *testing.T) {
	const src = `
[git]
strip_attribution = false
strip_attribution_patterns = ["^X-Bot:", "^Tool-Footer:"]
`
	var cfg Config
	if _, err := toml.Decode(src, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Git.IsStripAttributionEnabled() {
		t.Error("strip_attribution=false not parsed")
	}
	if len(cfg.Git.StripAttributionPatterns) != 2 {
		t.Errorf("patterns not parsed: %v", cfg.Git.StripAttributionPatterns)
	}
}

// Later scopes override the flag (pointer merge); a non-empty pattern list
// REPLACES the earlier one (the defaults apply only when no scope sets any).
func TestGitStripAttribution_Merge(t *testing.T) {
	f := false
	base := GetDefaultConfig()
	overlay := &Config{}
	overlay.Git.StripAttribution = &f
	overlay.Git.StripAttributionPatterns = []string{"^A:"}
	base.Merge(overlay)
	if base.Git.IsStripAttributionEnabled() {
		t.Error("overlay strip_attribution=false should win")
	}
	if len(base.Git.StripAttributionPatterns) != 1 || base.Git.StripAttributionPatterns[0] != "^A:" {
		t.Errorf("overlay patterns should replace, got %v", base.Git.StripAttributionPatterns)
	}
	overlay2 := &Config{}
	overlay2.Git.StripAttributionPatterns = []string{"^B:"}
	base.Merge(overlay2)
	if len(base.Git.StripAttributionPatterns) != 1 || base.Git.StripAttributionPatterns[0] != "^B:" {
		t.Errorf("later patterns should replace earlier, got %v", base.Git.StripAttributionPatterns)
	}
}

// Untrusted (project) config controls neither the flag nor the patterns — a
// cloned repo must not re-enable attribution the operator strips, nor choose
// arbitrary line-deletion patterns applied to every commit message.
func TestSanitizeUntrusted_GitStripAttribution(t *testing.T) {
	tr, f := true, false

	cfg := &Config{}
	cfg.Git.StripAttribution = &f
	cfg.Git.StripAttributionPatterns = []string{"^Anything:"}
	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")
	if cfg.Git.StripAttribution != nil {
		t.Error("untrusted strip_attribution=false must be stripped")
	}
	if cfg.Git.StripAttributionPatterns != nil {
		t.Error("untrusted strip_attribution_patterns must be stripped")
	}

	cfg = &Config{}
	cfg.Git.StripAttribution = &tr
	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")
	if cfg.Git.StripAttribution != nil {
		t.Error("untrusted strip_attribution=true must be stripped too (trusted scope only)")
	}
}

// forensics_on_kill is OPT-IN: default false, so an auto-kill deletes the
// container as before unless the operator asks to keep the evidence.
func TestForensicsOnKill_DefaultOffAndMerge(t *testing.T) {
	if GetDefaultConfig().Monitoring.IsForensicsOnKillEnabled() {
		t.Error("forensics_on_kill should default to disabled (opt-in)")
	}
	var nilCfg *MonitoringConfig
	if nilCfg.IsForensicsOnKillEnabled() {
		t.Error("nil monitoring config should report forensics disabled")
	}
	yes := true
	base := GetDefaultConfig()
	overlay := &Config{}
	overlay.Monitoring.ForensicsOnKill = &yes
	base.Merge(overlay)
	if !base.Monitoring.IsForensicsOnKillEnabled() {
		t.Error("overlay forensics_on_kill=true should win")
	}
}
