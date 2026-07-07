package vmhost

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name      string
		mounts    string
		user      string
		osRelease string
		want      Kind
	}{
		{
			name:   "lima virtiofs mount detected as lima-like",
			mounts: "mount0 on /Users/foo type virtiofs (rw,relatime)",
			want:   KindLimaLike,
		},
		{
			name: "lima user detected as lima-like",
			user: "lima",
			want: KindLimaLike,
		},
		{
			name:      "orbstack virtiofs mount detected as orbstack, not lima-like",
			mounts:    "mac on /Users type virtiofs (rw,relatime)",
			osRelease: "7.0.11-orbstack-00360-gc9bc4d96ac70",
			want:      KindOrbStack,
		},
		{
			name:      "no virtiofs, no lima user, no orbstack",
			mounts:    "/dev/sda1 on / type ext4 (rw,relatime)",
			osRelease: "6.8.0-generic",
			want:      KindUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect(tt.mounts, tt.user, tt.osRelease)
			if got != tt.want {
				t.Errorf("detect(%q, %q, %q) = %v, want %v", tt.mounts, tt.user, tt.osRelease, got, tt.want)
			}
		})
	}
}

func TestKindHandlesUIDMapping(t *testing.T) {
	tests := []struct {
		kind Kind
		want bool
	}{
		{KindUnknown, false},
		{KindLimaLike, true},
		{KindOrbStack, false},
	}

	for _, tt := range tests {
		if got := tt.kind.HandlesUIDMapping(); got != tt.want {
			t.Errorf("%v.HandlesUIDMapping() = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestKindString(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KindUnknown, ""},
		{KindLimaLike, "colima"},
		{KindOrbStack, "orbstack"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("%v.String() = %q, want %q", int(tt.kind), got, tt.want)
		}
	}
}

func TestDetect_Integration(t *testing.T) {
	// Just ensures Detect() runs against the real OS without panicking.
	kind := Detect()
	t.Logf("Detect() returned: %v (%q)", kind, kind.String())
}
