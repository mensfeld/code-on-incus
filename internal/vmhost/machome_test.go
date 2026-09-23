package vmhost

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMacHostMounts(t *testing.T) {
	tests := []struct {
		name   string
		mounts string
		want   []string
	}{
		{
			name: "colima virtiofs mac home",
			// Realistic Lima/Colima table: the Mac home is a virtiofs mount under
			// /Users; guest-local filesystems and /tmp/lima must be ignored.
			mounts: "" +
				"proc /proc proc rw,relatime 0 0\n" +
				"/dev/vda1 / ext4 rw,relatime 0 0\n" +
				"mount0 /Users/alice virtiofs rw,relatime 0 0\n" +
				"mount1 /tmp/lima virtiofs rw,relatime 0 0\n",
			want: []string{"/Users/alice"},
		},
		{
			name:   "lima 9p mount type",
			mounts: "share /Users/bob 9p rw,trans=virtio 0 0\n",
			want:   []string{"/Users/bob"},
		},
		{
			name: "whole /Users parent shared",
			mounts: "mount0 /Users virtiofs rw,relatime 0 0\n" +
				"/dev/vda1 / ext4 rw 0 0\n",
			want: []string{"/Users"},
		},
		{
			name: "sorted and deduplicated",
			mounts: "m2 /Users/carol virtiofs rw 0 0\n" +
				"m1 /Users/alice virtiofs rw 0 0\n" +
				"m1b /Users/alice virtiofs rw 0 0\n",
			want: []string{"/Users/alice", "/Users/carol"},
		},
		{
			name: "no mac shares (plain linux)",
			mounts: "proc /proc proc rw 0 0\n" +
				"/dev/sda1 / ext4 rw 0 0\n" +
				"tmpfs /run tmpfs rw 0 0\n",
			want: nil,
		},
		{
			name:   "non-virtiofs under /Users ignored",
			mounts: "tmpfs /Users/eve/scratch tmpfs rw 0 0\n",
			want:   nil,
		},
		{
			name:   "malformed lines skipped",
			mounts: "garbage\nmount0 /Users/frank virtiofs\n \n",
			want:   []string{"/Users/frank"},
		},
		{
			name:   "octal-escaped space in mountpoint decoded",
			mounts: `mount0 /Users/John\040Doe virtiofs rw 0 0` + "\n",
			want:   []string{"/Users/John Doe"},
		},
		{
			name: "escaped tab and backslash decoded",
			mounts: `m0 /Users/a\011b virtiofs rw 0 0` + "\n" +
				`m1 /Users/c\134d virtiofs rw 0 0` + "\n",
			want: []string{"/Users/a\tb", `/Users/c\d`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := macHostMounts(tt.mounts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("macHostMounts()\n got  %#v\n want %#v", got, tt.want)
			}
		})
	}
}

func TestBuildMacHostConfigCandidates(t *testing.T) {
	// listSubdirs stub keyed by directory.
	subdirsOf := func(m map[string][]string) func(string) []string {
		return func(p string) []string { return m[p] }
	}

	tests := []struct {
		name       string
		mounts     string
		configDir  string
		listSubdir func(string) []string
		want       []string
	}{
		{
			name:       "home mount does NOT descend (no whole-home read, no nested strays)",
			mounts:     "m0 /Users/alice virtiofs rw 0 0\n",
			configDir:  ".claude",
			listSubdir: subdirsOf(map[string][]string{"/Users/alice": {"Documents", "Projects"}}),
			want:       []string{"/Users/alice/.claude"},
		},
		{
			name:       "whole /Users parent descends one level",
			mounts:     "m0 /Users virtiofs rw 0 0\n",
			configDir:  ".claude",
			listSubdir: subdirsOf(map[string][]string{"/Users": {"alice", "bob"}}),
			want:       []string{"/Users/alice/.claude", "/Users/bob/.claude"},
		},
		{
			name:       "parent case produces no bogus /Users/.claude candidate",
			mounts:     "m0 /Users virtiofs rw 0 0\n",
			configDir:  ".claude",
			listSubdir: subdirsOf(map[string][]string{"/Users": {"alice"}}),
			want:       []string{"/Users/alice/.claude"},
		},
		{
			name:       "deduplicated across mounts",
			mounts:     "m0 /Users/alice virtiofs rw 0 0\nm1 /Users/alice 9p rw 0 0\n",
			configDir:  ".codex",
			listSubdir: subdirsOf(nil),
			want:       []string{"/Users/alice/.codex"},
		},
		{
			name:       "no mac mounts yields no candidates",
			mounts:     "/dev/sda1 / ext4 rw 0 0\n",
			configDir:  ".claude",
			listSubdir: subdirsOf(nil),
			want:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMacHostConfigCandidates(tt.mounts, tt.configDir, tt.listSubdir)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildMacHostConfigCandidates()\n got  %#v\n want %#v", got, tt.want)
			}
		})
	}
}

func TestDirHasAnyFile(t *testing.T) {
	dir := t.TempDir()

	// A dir with only a stray, non-config file must NOT count as populated —
	// otherwise a junk guest ~/.claude would shadow a real shared Mac home.
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	markers := []string{".credentials.json", "settings.json"}
	if dirHasAnyFile(dir, markers) {
		t.Error("dir with only a stray file should not be reported as having config")
	}

	// Once a marker file is present, it counts.
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !dirHasAnyFile(dir, markers) {
		t.Error("dir containing a marker file should be reported as having config")
	}

	// Missing directory is not populated, and never panics.
	if dirHasAnyFile(filepath.Join(dir, "does-not-exist"), markers) {
		t.Error("missing dir should not be reported as having config")
	}
	// No markers to look for -> never populated.
	if dirHasAnyFile(dir, nil) {
		t.Error("empty marker list should report no config")
	}
}

func TestResolveHostConfigDir(t *testing.T) {
	// nonEmpty stub: a path is "populated" iff it's in the set.
	nonEmptyIn := func(populated ...string) func(string) bool {
		set := make(map[string]bool, len(populated))
		for _, p := range populated {
			set[p] = true
		}
		return func(p string) bool { return set[p] }
	}

	const guest = "/home/lima/.claude"
	const mac = "/Users/alice/.claude"
	const mac2 = "/Users/bob/.claude"

	tests := []struct {
		name       string
		candidates []string
		nonEmpty   func(string) bool
		want       string
	}{
		{
			name:       "guest already populated wins over mac home",
			candidates: []string{mac},
			nonEmpty:   nonEmptyIn(guest, mac),
			want:       guest,
		},
		{
			name:       "empty guest falls back to populated mac home",
			candidates: []string{mac},
			nonEmpty:   nonEmptyIn(mac),
			want:       mac,
		},
		{
			name:       "nothing populated returns guest default unchanged",
			candidates: []string{mac},
			nonEmpty:   nonEmptyIn(),
			want:       guest,
		},
		{
			name:       "first populated candidate chosen deterministically",
			candidates: []string{mac, mac2},
			nonEmpty:   nonEmptyIn(mac, mac2),
			want:       mac,
		},
		{
			name:       "skips empty candidate to reach a populated one",
			candidates: []string{mac, mac2},
			nonEmpty:   nonEmptyIn(mac2),
			want:       mac2,
		},
		{
			name:       "candidate equal to guest path is ignored",
			candidates: []string{guest},
			nonEmpty:   nonEmptyIn(guest),
			want:       guest,
		},
		{
			name:       "no candidates returns guest",
			candidates: nil,
			nonEmpty:   nonEmptyIn(),
			want:       guest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveHostConfigDir(guest, tt.candidates, tt.nonEmpty)
			if got != tt.want {
				t.Fatalf("ResolveHostConfigDir() = %q, want %q", got, tt.want)
			}
		})
	}
}
