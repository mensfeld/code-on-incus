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

func TestBuildTmpMountUnit(t *testing.T) {
	got := buildTmpMountUnit(256 << 20)
	for _, want := range []string{
		"[Mount]",
		"What=tmpfs",
		"Where=/tmp",
		"Type=tmpfs",
		"size=268435456", // 256 MiB in bytes
		"mode=1777",
		"WantedBy=local-fs.target",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tmp.mount unit missing %q; got:\n%s", want, got)
		}
	}
}
