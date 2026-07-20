package container

import (
	"errors"
	"testing"
)

// TestIsNotFoundErr pins the behaviour that makes `coi kill` idempotent.
//
// Deletion races are routine: stopping a container ends the coi session that owns
// it, and that session then deletes its own ephemeral container. A concurrent
// `coi kill` can therefore find the instance already gone between checking that it
// exists and deleting it. Before this, that surfaced as "No containers were
// killed" and a non-zero exit — about a container that had, in fact, been killed.
func TestIsNotFoundErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "the real message Incus emits when the instance is gone",
			err: errors.New(`exit status 1: Error: Failed checking instance coi-614223fe-60 exists: ` +
				`Failed to fetch instance "coi-614223fe-60" in project "default": Instance not found`),
			want: true,
		},
		{
			name: "case-insensitive",
			err:  errors.New("Instance Not Found"),
			want: true,
		},
		{
			name: "nil is not a not-found",
			err:  nil,
			want: false,
		},
		{
			// The point of the helper is to swallow ONLY "it's already gone". A real
			// failure — the disk is busy, the daemon is down — must still be reported,
			// or `coi kill` would cheerfully claim success while leaving a live
			// container behind.
			name: "a genuine failure is not swallowed",
			err:  errors.New(`exit status 1: Error: Failed to destroy storage volume: device is busy`),
			want: false,
		},
		{
			name: "a daemon error is not swallowed",
			err:  errors.New("exit status 1: Error: Failed to connect to incusd"),
			want: false,
		},
		{
			name: "an unrelated 'not found' does not qualify",
			err:  errors.New("Error: snapshot not found"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNotFoundErr(tc.err); got != tc.want {
				t.Errorf("IsNotFoundErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
