package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// A per-mount `shift` override (#604) decodes to a *bool so an unset field is
// distinguishable from an explicit false, in both the flat and nested shapes.
func TestMounts_UnmarshalTOML_ShiftOverride(t *testing.T) {
	const in = `
[[mounts]]
host = "/a"
container = "/a"
shift = true
[[mounts]]
host = "/b"
container = "/b"
shift = false
[[mounts]]
host = "/c"
container = "/c"
`
	var cfg Config
	if _, err := toml.Decode(in, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := cfg.Mounts.Default
	if len(got) != 3 {
		t.Fatalf("want 3 mounts, got %d", len(got))
	}
	if got[0].Shift == nil || *got[0].Shift != true {
		t.Errorf("entry 0: want shift=true, got %v", got[0].Shift)
	}
	if got[1].Shift == nil || *got[1].Shift != false {
		t.Errorf("entry 1: want shift=false, got %v", got[1].Shift)
	}
	if got[2].Shift != nil {
		t.Errorf("entry 2: want shift unset (nil), got %v", *got[2].Shift)
	}
}

// A non-boolean `shift` is a decode error, not a silently-ignored key.
func TestMounts_UnmarshalTOML_ShiftMustBeBool(t *testing.T) {
	const in = `
[[mounts]]
host = "/a"
container = "/a"
shift = "yes"
`
	var cfg Config
	if _, err := toml.Decode(in, &cfg); err == nil {
		t.Fatal("expected error decoding non-boolean shift, got nil")
	}
}
