package container

import "testing"

// TestTmpMountIsBounded pins the /proc/mounts probe that backs coi's default
// /tmp cap (#728): a /tmp tmpfs with an explicit size= is "already bounded" and
// left untouched; an unbounded tmpfs, a disk-backed /tmp, or no /tmp line at
// all is NOT bounded, so coi applies its default cap.
func TestTmpMountIsBounded(t *testing.T) {
	cases := []struct {
		name   string
		mounts string
		want   bool
	}{
		{
			name:   "tmpfs with size= is bounded",
			mounts: "tmpfs /tmp tmpfs rw,nosuid,nodev,size=2097152k,mode=1777 0 0",
			want:   true,
		},
		{
			name:   "tmpfs without size= is unbounded",
			mounts: "tmpfs /tmp tmpfs rw,nosuid,nodev,mode=1777 0 0",
			want:   false,
		},
		{
			name:   "/tmp on rootfs (non-tmpfs) is not bounded here",
			mounts: "/dev/sda1 /tmp ext4 rw,relatime 0 0",
			want:   false,
		},
		{
			name:   "no /tmp line at all",
			mounts: "proc /proc proc rw 0 0\nsysfs /sys sysfs rw 0 0",
			want:   false,
		},
		{
			name: "finds /tmp among many lines",
			mounts: "proc /proc proc rw 0 0\n" +
				"tmpfs /tmp tmpfs rw,size=1048576k 0 0\n" +
				"sysfs /sys sysfs rw 0 0",
			want: true,
		},
		{
			name:   "does not confuse /tmpfoo with /tmp",
			mounts: "tmpfs /tmpfoo tmpfs rw,size=1048576k 0 0",
			want:   false,
		},
		{
			name:   "empty input",
			mounts: "",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tmpMountIsBounded(tc.mounts); got != tc.want {
				t.Errorf("tmpMountIsBounded() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTmpHasSubmount pins the guard that stops the default /tmp cap from
// shadowing anything mounted under /tmp — notably a preserve_workspace_path
// workspace mounted at /tmp/<...>/workspace inside the container (#728).
func TestTmpHasSubmount(t *testing.T) {
	cases := []struct {
		name   string
		mounts string
		want   bool
	}{
		{
			name:   "workspace mounted under /tmp",
			mounts: "/dev/sda1 /tmp/pytest-of-runner/pytest-0/ws/workspace ext4 rw 0 0",
			want:   true,
		},
		{
			name:   "only /tmp itself, no submount",
			mounts: "tmpfs /tmp tmpfs rw,mode=1777 0 0",
			want:   false,
		},
		{
			name:   "workspace at /workspace (not under /tmp)",
			mounts: "/dev/sda1 /workspace ext4 rw 0 0\ntmpfs /tmp tmpfs rw 0 0",
			want:   false,
		},
		{
			name:   "does not match /tmpfoo (needs the slash)",
			mounts: "/dev/sda1 /tmpfoo ext4 rw 0 0",
			want:   false,
		},
		{
			name:   "empty input",
			mounts: "",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tmpHasSubmount(tc.mounts); got != tc.want {
				t.Errorf("tmpHasSubmount() = %v, want %v", got, tc.want)
			}
		})
	}
}
