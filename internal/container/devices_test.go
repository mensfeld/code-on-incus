package container

import (
	"reflect"
	"testing"
)

func TestParseDiskDeviceSources(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantSources []string
		wantShift   bool
	}{
		{
			name: "workspace disk shift=true",
			yaml: `workspace:
  path: /workspace
  source: /home/user/project
  type: disk
  shift: "true"`,
			wantSources: []string{"/home/user/project"},
			wantShift:   true,
		},
		{
			name: "workspace disk shift=false",
			yaml: `workspace:
  path: /workspace
  source: /home/user/project
  type: disk
  shift: "false"`,
			wantSources: []string{"/home/user/project"},
			wantShift:   false,
		},
		{
			name: "disk with no source excluded from sources",
			yaml: `root:
  path: /
  pool: default
  type: disk`,
			wantSources: nil,
			wantShift:   false,
		},
		{
			name: "non-disk device ignored for source and shift",
			yaml: `myproxy:
  type: proxy
  source: /tmp/host.sock
  shift: "true"
workspace:
  type: disk
  source: /home/user/project
  shift: "false"`,
			wantSources: []string{"/home/user/project"},
			wantShift:   false,
		},
		{
			// Multiple disks, mixed shift → all sources (sorted by device name),
			// hasShift true if ANY disk is shifted.
			name: "multiple disks mixed shift, sorted sources",
			yaml: `workspace:
  type: disk
  source: /home/user/project
  shift: "true"
cache:
  type: disk
  source: /home/user/.cache
  shift: "false"`,
			wantSources: []string{"/home/user/.cache", "/home/user/project"}, // sorted: cache before workspace
			wantShift:   true,
		},
		{
			name:        "empty string",
			yaml:        "",
			wantSources: nil,
			wantShift:   false,
		},
		{
			name:        "empty map",
			yaml:        "{}",
			wantSources: nil,
			wantShift:   false,
		},
		{
			name:        "malformed yaml",
			yaml:        "this: : : not valid",
			wantSources: nil,
			wantShift:   false,
		},
		{
			name: "disk with shift absent",
			yaml: `workspace:
  type: disk
  source: /home/user/project`,
			wantSources: []string{"/home/user/project"},
			wantShift:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources, hasShift := parseDiskDeviceSources(tt.yaml)
			if !reflect.DeepEqual(sources, tt.wantSources) {
				t.Errorf("sources = %#v, want %#v", sources, tt.wantSources)
			}
			if hasShift != tt.wantShift {
				t.Errorf("hasShiftDevice = %v, want %v", hasShift, tt.wantShift)
			}
		})
	}
}
