package session

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fakeReconciler implements protectedDeviceReconciler for ReconcileProtectedDevices.
type fakeReconciler struct {
	sources map[string]string // device name -> host source
	removed []string
}

func (f *fakeReconciler) ListDevices() ([]string, error) {
	names := make([]string, 0, len(f.sources))
	for n := range f.sources {
		names = append(names, n)
	}
	return names, nil
}

func (f *fakeReconciler) RemoveDevice(name string) error {
	f.removed = append(f.removed, name)
	delete(f.sources, name)
	return nil
}

func (f *fakeReconciler) GetDeviceSource(name string) (string, error) {
	return f.sources[name], nil
}

// Issue #610: on reuse, a protect-* device whose workspace source was removed
// must be repaired so Incus does not reject the container at start. Default
// paths are re-materialized (protection preserved); user-added / non-default
// paths have their device removed (nothing left to protect); devices whose
// source is still present, and non-protect devices, are left untouched.
func TestReconcileProtectedDevices(t *testing.T) {
	tmp := t.TempDir()

	// A default protected path whose source is still present must be preserved:
	// materialize .vscode up front.
	if err := os.Mkdir(filepath.Join(tmp, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}

	fr := &fakeReconciler{sources: map[string]string{
		// default dir-type, source MISSING -> must be re-materialized, kept
		"protect-husky": filepath.Join(tmp, ".husky"),
		// default file-type (auto-create parent), source MISSING -> re-materialized, kept
		"protect-claude-settingsjson": filepath.Join(tmp, ".claude", "settings.json"),
		// user-added path, source MISSING -> device removed
		"protect-idea": filepath.Join(tmp, ".idea"),
		// default dir-type, source PRESENT -> untouched
		"protect-vscode": filepath.Join(tmp, ".vscode"),
		// non-protect device -> ignored entirely even though its source is gone
		"workspace": filepath.Join(tmp, "does-not-exist"),
	}}

	ReconcileProtectedDevices(fr, tmp, func(string) {})

	// Default paths with a missing source are re-materialized on the host and
	// their devices kept (NOT removed).
	if info, err := os.Lstat(filepath.Join(tmp, ".husky")); err != nil || !info.IsDir() {
		t.Errorf(".husky should have been re-materialized as a dir: err=%v", err)
	}
	if info, err := os.Lstat(filepath.Join(tmp, ".claude", "settings.json")); err != nil || info.IsDir() {
		t.Errorf(".claude/settings.json should have been re-materialized as a file: err=%v", err)
	}
	for _, kept := range []string{"protect-husky", "protect-claude-settingsjson"} {
		if slices.Contains(fr.removed, kept) {
			t.Errorf("%s device must be kept after re-materialization, but it was removed", kept)
		}
	}

	// User-added path with a missing source: device removed, and NOT materialized.
	if !slices.Contains(fr.removed, "protect-idea") {
		t.Errorf("protect-idea should have been removed (user path, source gone); removed=%v", fr.removed)
	}
	if _, err := os.Lstat(filepath.Join(tmp, ".idea")); !os.IsNotExist(err) {
		t.Errorf(".idea must NOT be materialized for a user-added path (err=%v)", err)
	}

	// Present-source and non-protect devices are untouched.
	if slices.Contains(fr.removed, "protect-vscode") {
		t.Error("protect-vscode (source present) must not be removed")
	}
	if slices.Contains(fr.removed, "workspace") {
		t.Error("non-protect device 'workspace' must never be touched by the reconcile")
	}
}

// A .git/* protected device must not resurrect a .git tree when the workspace
// .git is gone: the device is removed instead of synthesizing a directory.
func TestReconcileProtectedDevices_DoesNotSynthesizeGit(t *testing.T) {
	tmp := t.TempDir() // no .git here

	fr := &fakeReconciler{sources: map[string]string{
		"protect-git-hooks": filepath.Join(tmp, ".git", "hooks"),
	}}

	ReconcileProtectedDevices(fr, tmp, func(string) {})

	if _, err := os.Lstat(filepath.Join(tmp, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git must not be synthesized during reconcile (err=%v)", err)
	}
	if !slices.Contains(fr.removed, "protect-git-hooks") {
		t.Errorf("protect-git-hooks should have been removed when .git is absent; removed=%v", fr.removed)
	}
}
