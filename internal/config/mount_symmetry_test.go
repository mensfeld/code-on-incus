package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// wantMounts is the expected decode of the shared two-entry fixture used below.
func assertFixtureMounts(t *testing.T, got []MountEntry) {
	t.Helper()
	if len(got) != 2 {
		t.Fatalf("want 2 mounts, got %d: %+v", len(got), got)
	}
	if got[0].Host != "/h1" || got[0].Container != "/c1" || !got[0].Readonly {
		t.Errorf("entry 0: got %+v, want {/h1 /c1 readonly}", got[0])
	}
	if got[1].Host != "/h2" || got[1].Container != "/c2" || got[1].Readonly {
		t.Errorf("entry 1: got %+v, want {/h2 /c2 rw}", got[1])
	}
}

// Top-level [mounts] accepts BOTH the flat [[mounts]] array form and the nested
// [[mounts.default]] form, decoding to the same result — the core of the
// config/profile mount symmetry fix.
func TestMountsConfig_UnmarshalTOML_BothShapes(t *testing.T) {
	const flat = `
[[mounts]]
host = "/h1"
container = "/c1"
readonly = true
[[mounts]]
host = "/h2"
container = "/c2"
`
	const nested = `
[[mounts.default]]
host = "/h1"
container = "/c1"
readonly = true
[[mounts.default]]
host = "/h2"
container = "/c2"
`
	for name, in := range map[string]string{"flat": flat, "nested": nested} {
		t.Run(name, func(t *testing.T) {
			var cfg Config
			if _, err := toml.Decode(in, &cfg); err != nil {
				t.Fatalf("decode: %v", err)
			}
			assertFixtureMounts(t, cfg.Mounts.Default)
		})
	}
}

// A profile's `mounts` key accepts the same two shapes as top-level [mounts],
// so a mount block can be copied between scopes verbatim.
func TestProfileMounts_UnmarshalTOML_BothShapes(t *testing.T) {
	const flat = `
[[mounts]]
host = "/h1"
container = "/c1"
readonly = true
[[mounts]]
host = "/h2"
container = "/c2"
`
	const nested = `
[[mounts.default]]
host = "/h1"
container = "/c1"
readonly = true
[[mounts.default]]
host = "/h2"
container = "/c2"
`
	for name, in := range map[string]string{"flat": flat, "nested": nested} {
		t.Run(name, func(t *testing.T) {
			var p ProfileConfig
			if _, err := toml.Decode(in, &p); err != nil {
				t.Fatalf("decode: %v", err)
			}
			assertFixtureMounts(t, p.Mounts)
		})
	}
}

// A bare [mounts] table with no entries decodes to no mounts, not an error.
func TestMountsConfig_UnmarshalTOML_Empty(t *testing.T) {
	for name, in := range map[string]string{
		"empty table":         "[mounts]\n",
		"empty default array": "[mounts]\ndefault = []\n",
	} {
		t.Run(name, func(t *testing.T) {
			var cfg Config
			if _, err := toml.Decode(in, &cfg); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(cfg.Mounts.Default) != 0 {
				t.Errorf("want no mounts, got %+v", cfg.Mounts.Default)
			}
		})
	}
}

// A wrong-typed field inside a mount entry fails loudly at decode time rather
// than silently producing a zero-valued mount.
func TestMountsConfig_UnmarshalTOML_TypeErrors(t *testing.T) {
	cases := map[string]string{
		"host not string":    "[[mounts]]\nhost = 3\ncontainer = \"/c\"\n",
		"readonly not bool":  "[[mounts]]\nhost = \"/h\"\ncontainer = \"/c\"\nreadonly = \"yes\"\n",
		"mounts is a string": "mounts = \"nope\"\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var cfg Config
			if _, err := toml.Decode(in, &cfg); err == nil {
				t.Fatal("expected a decode error")
			}
		})
	}
}

// The flat and nested forms decode to byte-identical mount slices — the
// guarantee that makes the two scopes interchangeable.
func TestMounts_FlatAndNestedAreEquivalent(t *testing.T) {
	const flat = "[[mounts]]\nhost=\"/a\"\ncontainer=\"/b\"\nreadonly=true\n"
	const nested = "[[mounts.default]]\nhost=\"/a\"\ncontainer=\"/b\"\nreadonly=true\n"

	var a, b Config
	if _, err := toml.Decode(flat, &a); err != nil {
		t.Fatalf("flat decode: %v", err)
	}
	if _, err := toml.Decode(nested, &b); err != nil {
		t.Fatalf("nested decode: %v", err)
	}
	if len(a.Mounts.Default) != len(b.Mounts.Default) {
		t.Fatalf("length mismatch: flat=%d nested=%d", len(a.Mounts.Default), len(b.Mounts.Default))
	}
	for i := range a.Mounts.Default {
		if a.Mounts.Default[i] != b.Mounts.Default[i] {
			t.Errorf("entry %d differs: flat=%+v nested=%+v", i, a.Mounts.Default[i], b.Mounts.Default[i])
		}
	}
}
