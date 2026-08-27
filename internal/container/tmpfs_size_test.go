package container

import (
	"strings"
	"testing"
)

func TestParseSizeBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"256MiB", 256 << 20, true},
		{"2GiB", 2 << 30, true},
		{"1024MiB", 1024 << 20, true},
		{"512KiB", 512 << 10, true},
		{"1TiB", 1 << 40, true},
		{"256M", 256 << 20, true},        // bare binary suffix
		{"2G", 2 << 30, true},            // bare binary suffix
		{"1000MB", 1000 * 1e6, true},     // SI (decimal)
		{"1GB", 1e9, true},               // SI
		{"1073741824", 1073741824, true}, // plain bytes
		{"1073741824B", 1073741824, true},
		{" 256MiB ", 256 << 20, true}, // surrounding space
		{"256mib", 256 << 20, true},   // case-insensitive
		{"", 0, false},
		{"abc", 0, false},
		{"12x", 0, false},
	}
	for _, tt := range tests {
		got, err := parseSizeBytes(tt.in)
		if tt.ok {
			if err != nil {
				t.Errorf("parseSizeBytes(%q) unexpected error: %v", tt.in, err)
				continue
			}
			if got != tt.want {
				t.Errorf("parseSizeBytes(%q) = %d, want %d", tt.in, got, tt.want)
			}
		} else if err == nil {
			t.Errorf("parseSizeBytes(%q) = %d, want error", tt.in, got)
		}
	}
}

func TestBuildTmpfsRawLXC(t *testing.T) {
	const wantEntry = "lxc.mount.entry = tmpfs tmp tmpfs rw,nosuid,nodev,size=268435456,mode=1777,create=dir 0 0"

	// From empty raw.lxc: just the tmpfs entry.
	if got := buildTmpfsRawLXC("", 256<<20); got != wantEntry {
		t.Errorf("empty base:\n got %q\nwant %q", got, wantEntry)
	}

	// Preserves unrelated existing raw.lxc lines, appends the tmpfs entry.
	base := "lxc.some.other = value\n"
	got := buildTmpfsRawLXC(base, 256<<20)
	if !strings.Contains(got, "lxc.some.other = value") {
		t.Errorf("dropped an unrelated raw.lxc line: %q", got)
	}
	if !strings.Contains(got, wantEntry) {
		t.Errorf("missing tmpfs entry: %q", got)
	}

	// Replaces (does not duplicate) a prior /tmp tmpfs entry.
	prior := "lxc.mount.entry = tmpfs tmp tmpfs rw,size=99999,create=dir 0 0"
	got = buildTmpfsRawLXC(prior, 256<<20)
	if strings.Count(got, "tmp tmpfs") != 1 {
		t.Errorf("expected exactly one /tmp tmpfs entry, got:\n%s", got)
	}
	if !strings.Contains(got, "size=268435456") || strings.Contains(got, "size=99999") {
		t.Errorf("prior tmpfs entry not replaced: %q", got)
	}
}
