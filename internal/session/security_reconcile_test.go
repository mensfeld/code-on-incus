package session

import (
	"slices"
	"testing"
)

// fakeStripper implements securityDeviceStripper for StripSecurityDevices.
type fakeStripper struct {
	devices []string
	removed []string
}

func (f *fakeStripper) ListDevices() ([]string, error) { return f.devices, nil }
func (f *fakeStripper) RemoveDevice(name string) error {
	f.removed = append(f.removed, name)
	return nil
}

// Issue #610: on reuse, StripSecurityDevices must remove exactly the security
// device families the fresh-launch setup re-establishes (protect-*, mask-*,
// gitc-*) and NOTHING else — in particular it must never remove the read-write
// base mounts those overlay (workspace, git-worktree-common) or unrelated
// devices (coi-port-*, tmp, root, eth0).
func TestStripSecurityDevices(t *testing.T) {
	fr := &fakeStripper{devices: []string{
		"protect-husky",
		"protect-claude-settingsjson",
		"mask-abc123",
		"gitc-deadbeef",
		// must be KEPT:
		"workspace",
		"git-worktree-common",
		"coi-port-web",
		"tmp",
		"root",
		"eth0",
	}}

	StripSecurityDevices(fr, func(string) {})

	wantRemoved := []string{
		"protect-husky", "protect-claude-settingsjson", "mask-abc123", "gitc-deadbeef",
	}
	for _, d := range wantRemoved {
		if !slices.Contains(fr.removed, d) {
			t.Errorf("expected %s to be stripped; removed=%v", d, fr.removed)
		}
	}

	mustKeep := []string{"workspace", "git-worktree-common", "coi-port-web", "tmp", "root", "eth0"}
	for _, d := range mustKeep {
		if slices.Contains(fr.removed, d) {
			t.Errorf("%s must NOT be stripped (it is a base mount or unrelated device); removed=%v", d, fr.removed)
		}
	}

	if len(fr.removed) != len(wantRemoved) {
		t.Errorf("stripped %d devices, want exactly %d (%v)", len(fr.removed), len(wantRemoved), fr.removed)
	}
}
