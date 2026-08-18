package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/session"
)

func TestGitConfigQuote(t *testing.T) {
	cases := map[string]string{
		"coipond-coder[bot]": `"coipond-coder[bot]"`, // brackets are ordinary in a value
		`a"b`:                `"a\"b"`,               // quote escaped
		`a\b`:                `"a\\b"`,               // backslash escaped
		"plain":              `"plain"`,
	}
	for in, want := range cases {
		if got := gitConfigQuote(in); got != want {
			t.Errorf("gitConfigQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderReadonlyGitConfig(t *testing.T) {
	got := renderReadonlyGitConfig(session.GitIdentity{
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

func TestBuildReadonlyGitMount(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // don't touch the real ~/.coi
	id := session.GitIdentity{Name: "Bot", Email: "bot@example.com"}

	m, err := buildReadonlyGitMount(id, "/home/code")
	if err != nil {
		t.Fatalf("buildReadonlyGitMount: %v", err)
	}
	if !m.Readonly {
		t.Error("git mount must be read-only")
	}
	if m.ContainerPath != "/home/code/.gitconfig" {
		t.Errorf("container path = %q, want /home/code/.gitconfig", m.ContainerPath)
	}
	if m.DeviceName != "git-identity" {
		t.Errorf("device name = %q, want git-identity", m.DeviceName)
	}
	// The host file must exist under ~/.coi and hold the rendered identity.
	if !strings.Contains(m.HostPath, filepath.Join(".coi", "git-identity")) {
		t.Errorf("host path should live under ~/.coi/git-identity: %q", m.HostPath)
	}
	data, err := os.ReadFile(m.HostPath)
	if err != nil {
		t.Fatalf("reading generated host gitconfig: %v", err)
	}
	if !strings.Contains(string(data), `name = "Bot"`) || !strings.Contains(string(data), "useConfigOnly = true") {
		t.Errorf("generated host gitconfig content unexpected:\n%s", data)
	}

	// Distinct identities must not collide on the same host path.
	m2, err := buildReadonlyGitMount(session.GitIdentity{Name: "Other", Email: "o@example.com"}, "/home/code")
	if err != nil {
		t.Fatal(err)
	}
	if m2.HostPath == m.HostPath {
		t.Error("distinct identities must map to distinct host files")
	}
}
