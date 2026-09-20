package session

import "testing"

func boolPtr(b bool) *bool { return &b }

// ResolveMountShift folds a per-mount `shift` override (#604) into the
// session-wide decision, with the raw.idmap mutual-exclusivity guard.
func TestResolveMountShift(t *testing.T) {
	cases := []struct {
		name         string
		sessionShift bool
		override     *bool
		rawIdmap     bool
		want         bool
		wantWarn     bool
	}{
		{"unset inherits session (on)", true, nil, false, true, false},
		{"unset inherits session (off)", false, nil, false, false, false},
		{"override true forces on", false, boolPtr(true), false, true, false},
		{"override false forces off", true, boolPtr(false), false, false, false},
		{"override true dropped under raw.idmap", false, boolPtr(true), true, false, true},
		{"override false is a no-op under raw.idmap", false, boolPtr(false), true, false, false},
		{"unset under raw.idmap inherits off", false, nil, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warned := false
			logger := func(string) { warned = true }
			got := ResolveMountShift(tc.sessionShift, tc.override, tc.rawIdmap, "mount-0", logger)
			if got != tc.want {
				t.Errorf("got shift=%v, want %v", got, tc.want)
			}
			if warned != tc.wantWarn {
				t.Errorf("got warn=%v, want %v", warned, tc.wantWarn)
			}
		})
	}
}

// A nil logger must not panic when the guard fires.
func TestResolveMountShift_NilLoggerSafe(t *testing.T) {
	if got := ResolveMountShift(false, boolPtr(true), true, "mount-0", nil); got != false {
		t.Errorf("got %v, want false", got)
	}
}

// setupMounts must apply each mount's effective shift flag to MountDisk: a
// per-mount override wins over the session-wide default, while an unset mount
// inherits it (#604).
func TestSetupMounts_AppliesPerMountShift(t *testing.T) {
	rec := &recordingDevices{}
	mc := &MountConfig{Mounts: []MountEntry{
		{DeviceName: "mount-0", HostPath: t.TempDir(), ContainerPath: "/a"},                       // inherits session (off)
		{DeviceName: "mount-1", HostPath: t.TempDir(), ContainerPath: "/b", Shift: boolPtr(true)}, // forces on
	}}
	// Session default is OFF; only the overridden mount should be shifted.
	if err := setupMounts(rec, mc, false, false, func(string) {}); err != nil {
		t.Fatalf("setupMounts: %v", err)
	}
	if len(rec.mounts) != 2 {
		t.Fatalf("want 2 MountDisk calls, got %d", len(rec.mounts))
	}
	if rec.mounts[0].shift {
		t.Errorf("mount-0 should inherit session shift=false, got shift=true")
	}
	if !rec.mounts[1].shift {
		t.Errorf("mount-1 override shift=true not applied, got shift=false")
	}
}

// Under raw.idmap, a per-mount shift=true is clamped to false at the MountDisk
// boundary so the container is never handed a shift+raw.idmap combination.
func TestSetupMounts_ShiftClampedUnderRawIdmap(t *testing.T) {
	rec := &recordingDevices{}
	mc := &MountConfig{Mounts: []MountEntry{
		{DeviceName: "mount-0", HostPath: t.TempDir(), ContainerPath: "/a", Shift: boolPtr(true)},
	}}
	if err := setupMounts(rec, mc, false, true /* rawIdmapActive */, func(string) {}); err != nil {
		t.Fatalf("setupMounts: %v", err)
	}
	if rec.mounts[0].shift {
		t.Errorf("shift=true must be clamped to false under raw.idmap, got shift=true")
	}
}
