package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitConfigQuote(t *testing.T) {
	cases := map[string]string{
		"coipond-coder[bot]": `"coipond-coder[bot]"`, // brackets are ordinary in a value
		`a"b`:                `"a\"b"`,               // quote escaped
		`a\b`:                `"a\\b"`,               // backslash escaped
		"a\nb":               `"a\nb"`,               // newline escaped (no broken multi-line entry)
		"plain":              `"plain"`,
	}
	for in, want := range cases {
		if got := gitConfigQuote(in); got != want {
			t.Errorf("gitConfigQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderReadonlyGitConfig(t *testing.T) {
	got := renderReadonlyGitConfig(GitIdentity{
		Name:  "coipond-coder[bot]",
		Email: "4624853+coipond-coder[bot]@users.noreply.github.com",
	})
	for _, want := range []string{
		`name = "coipond-coder[bot]"`,
		`email = "4624853+coipond-coder[bot]@users.noreply.github.com"`,
		"useConfigOnly = true",
		"[user]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config missing %q:\n%s", want, got)
		}
	}
}

func TestReadonlyGitConfigHostPath_DistinctPerIdentity(t *testing.T) {
	a := readonlyGitConfigHostPath("/home/u", GitIdentity{Name: "A", Email: "a@x"})
	b := readonlyGitConfigHostPath("/home/u", GitIdentity{Name: "B", Email: "b@x"})
	if a == b {
		t.Error("distinct identities must map to distinct host paths")
	}
	if !strings.Contains(a, filepath.Join(".coi", "git-identity")) {
		t.Errorf("host path should live under ~/.coi/git-identity: %q", a)
	}
}

// stubMounter records the device operations so SetupGitIdentityReadonly can be
// exercised without a real container.
type stubMounter struct {
	removed     []string
	mounted     bool
	source      string
	path        string
	readonly    bool
	mountErr    error
	mountCalled bool
}

func (s *stubMounter) RemoveDevice(name string) error {
	s.removed = append(s.removed, name)
	return nil
}

func (s *stubMounter) MountDisk(name, source, path string, _, readonly bool) error {
	s.mountCalled = true
	s.mounted = true
	s.source = source
	s.path = path
	s.readonly = readonly
	return s.mountErr
}

func TestSetupGitIdentityReadonly(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // don't touch the real ~/.coi
	id := GitIdentity{Name: "Bot", Email: "bot@example.com"}

	t.Run("removes stale device then mounts read-only at the resolved home", func(t *testing.T) {
		m := &stubMounter{}
		if err := SetupGitIdentityReadonly(m, "/root", id); err != nil {
			t.Fatalf("SetupGitIdentityReadonly: %v", err)
		}
		if len(m.removed) != 1 || m.removed[0] != gitReadonlyDeviceName {
			t.Errorf("must remove the stale git-identity device first, got %v", m.removed)
		}
		if m.path != "/root/.gitconfig" {
			t.Errorf("mount must target the RESOLVED home (/root here), got %q", m.path)
		}
		if !m.readonly {
			t.Error("mount must be read-only")
		}
		data, err := os.ReadFile(m.source)
		if err != nil {
			t.Fatalf("host gitconfig should exist: %v", err)
		}
		if !strings.Contains(string(data), `name = "Bot"`) {
			t.Errorf("host gitconfig content unexpected:\n%s", data)
		}
	})

	t.Run("fails closed when the mount fails", func(t *testing.T) {
		m := &stubMounter{mountErr: errors.New("incus boom")}
		if err := SetupGitIdentityReadonly(m, "/home/code", id); err == nil {
			t.Fatal("a mount failure must return an error (fail closed), not nil")
		}
	})
}
