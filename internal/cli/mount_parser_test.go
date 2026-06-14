package cli

import (
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// Project-scoped (untrusted) mounts must be confined to the workspace; user
// mounts are unrestricted.
func TestParseMountConfig_ConfinesProjectMounts(t *testing.T) {
	const ws = "/home/user/project"

	cases := []struct {
		name    string
		mount   config.MountEntry
		wantErr bool
	}{
		{
			name:  "project mount inside workspace allowed",
			mount: config.MountEntry{Host: "/home/user/project/data", Container: "/data", FromProject: true},
		},
		{
			name:  "project mount equal to workspace allowed",
			mount: config.MountEntry{Host: "/home/user/project", Container: "/data", FromProject: true},
		},
		{
			name:    "project mount to /etc rejected",
			mount:   config.MountEntry{Host: "/etc", Container: "/mnt/etc", FromProject: true},
			wantErr: true,
		},
		{
			name:    "project mount to host home rejected",
			mount:   config.MountEntry{Host: "/home/user", Container: "/mnt/home", FromProject: true},
			wantErr: true,
		},
		{
			name:    "project mount traversal escape rejected",
			mount:   config.MountEntry{Host: "/home/user/project/../secret", Container: "/mnt/x", FromProject: true},
			wantErr: true,
		},
		{
			name:  "user (non-project) mount outside workspace allowed",
			mount: config.MountEntry{Host: "/etc", Container: "/mnt/etc", FromProject: false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.GetDefaultConfig()
			cfg.Mounts.Default = []config.MountEntry{tc.mount}

			_, err := ParseMountConfig(cfg, ws)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.mount.Host)
				}
				if !strings.Contains(err.Error(), "escapes the workspace") {
					t.Errorf("expected workspace-escape error, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
