package session

import (
	"errors"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// createWithOwnerCall records one CreateFileWithOwner invocation.
type createWithOwnerCall struct {
	path    string
	content string
	uid     int
	gid     int
	mode    string
}

// managedSettingsRecorder implements the ExecCommand and CreateFileWithOwner
// methods SetupClaudeManagedSettings uses; the embedded interface panics on
// anything else so unexpected calls fail loudly.
type managedSettingsRecorder struct {
	container.ContainerManager
	commands []string
	creates  []createWithOwnerCall
	// execErr, if set, decides whether a given in-container command fails.
	execErr func(command string) error
	// createErr, if set, fails every CreateFileWithOwner call.
	createErr error
}

func (r *managedSettingsRecorder) ExecCommand(command string, _ container.ExecCommandOptions) (string, error) {
	r.commands = append(r.commands, command)
	if r.execErr != nil {
		if err := r.execErr(command); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (r *managedSettingsRecorder) CreateFileWithOwner(path, content string, uid, gid int, mode string) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.creates = append(r.creates, createWithOwnerCall{path: path, content: content, uid: uid, gid: gid, mode: mode})
	return nil
}

// A plain CreateFile push inherits the host temp file's mode (0600) and UID,
// which the container code user cannot read when the host UID differs from it
// (macOS 501, CI runners 1001) — Claude Code then fails OAuth with EACCES on
// the policy file. Setup must push the file root-owned and world-readable in
// one operation so no window or failure path leaves it unreadable.
func TestSetupClaudeManagedSettingsCreatesRootOwnedWorldReadableFile(t *testing.T) {
	rec := &managedSettingsRecorder{}

	SetupClaudeManagedSettings(rec, func(msg string) { t.Log(msg) })

	if len(rec.creates) != 1 {
		t.Fatalf("expected exactly one CreateFileWithOwner call, got %v", rec.creates)
	}
	got := rec.creates[0]
	if got.path != "/etc/claude-code/managed-settings.json" {
		t.Errorf("path = %q, want /etc/claude-code/managed-settings.json", got.path)
	}
	if !strings.Contains(got.content, "disableAutoMode") {
		t.Errorf("unexpected managed settings content: %s", got.content)
	}
	if got.uid != 0 || got.gid != 0 {
		t.Errorf("owner = %d:%d, want 0:0", got.uid, got.gid)
	}
	if got.mode != "0644" {
		t.Errorf("mode = %q, want 0644", got.mode)
	}
}

func TestSetupClaudeManagedSettingsWarnsWhenWriteFails(t *testing.T) {
	rec := &managedSettingsRecorder{createErr: errors.New("push failed")}
	var logs []string

	SetupClaudeManagedSettings(rec, func(msg string) { logs = append(logs, msg) })

	if len(logs) == 0 {
		t.Fatal("expected a warning log when writing managed settings fails")
	}
}

func TestSetupClaudeManagedSettingsSkipsWriteWhenMkdirFails(t *testing.T) {
	rec := &managedSettingsRecorder{execErr: func(cmd string) error {
		if strings.Contains(cmd, "mkdir") {
			return errors.New("mkdir failed")
		}
		return nil
	}}
	var logs []string

	SetupClaudeManagedSettings(rec, func(msg string) { logs = append(logs, msg) })

	if len(rec.creates) != 0 {
		t.Fatalf("no file should be written after mkdir failure, got %v", rec.creates)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "directory") {
		t.Fatalf("expected exactly one mkdir-failure warning, got %v", logs)
	}
}
