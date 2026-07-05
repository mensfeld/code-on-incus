package session

import "testing"

func TestDetectColimaOrLima(t *testing.T) {
	tests := []struct {
		name      string
		mounts    string
		user      string
		osRelease string
		want      bool
	}{
		{
			name:   "lima virtiofs mount detected as lima/colima",
			mounts: "mount0 on /Users/foo type virtiofs (rw,relatime)",
			want:   true,
		},
		{
			name: "lima user detected as lima/colima",
			user: "lima",
			want: true,
		},
		{
			name:      "orbstack virtiofs mount is not misdetected as lima/colima",
			mounts:    "mac on /Users type virtiofs (rw,relatime)",
			osRelease: "7.0.11-orbstack-00360-gc9bc4d96ac70",
			want:      false,
		},
		{
			name:      "no virtiofs, no lima user, no orbstack",
			mounts:    "/dev/sda1 on / type ext4 (rw,relatime)",
			osRelease: "6.8.0-generic",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectColimaOrLima(tt.mounts, tt.user, tt.osRelease)
			if got != tt.want {
				t.Errorf("detectColimaOrLima(%q, %q, %q) = %v, want %v", tt.mounts, tt.user, tt.osRelease, got, tt.want)
			}
		})
	}
}
