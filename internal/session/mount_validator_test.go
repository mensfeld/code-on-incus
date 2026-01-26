package session

import "testing"

func TestValidateMounts_NoNesting(t *testing.T) {
	config := &MountConfig{
		Mounts: []MountEntry{
			{ContainerPath: "/data1"},
			{ContainerPath: "/data2"},
			{ContainerPath: "/app"},
		},
	}

	if err := ValidateMounts(config); err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestValidateMounts_DetectsNesting(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
	}{
		{"parent-child", []string{"/data", "/data/subdir"}},
		{"child-parent", []string{"/data/subdir", "/data"}},
		{"exact-duplicate", []string{"/data", "/data"}},
		{"deep-nesting", []string{"/a/b/c", "/a/b/c/d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mounts := make([]MountEntry, len(tt.paths))
			for i, p := range tt.paths {
				mounts[i] = MountEntry{ContainerPath: p}
			}

			config := &MountConfig{Mounts: mounts}
			if err := ValidateMounts(config); err == nil {
				t.Errorf("Expected error for nested paths %v", tt.paths)
			}
		})
	}
}

func TestValidateMounts_SimilarNamesOK(t *testing.T) {
	config := &MountConfig{
		Mounts: []MountEntry{
			{ContainerPath: "/data"},
			{ContainerPath: "/data-backup"},
			{ContainerPath: "/app"},
			{ContainerPath: "/application"},
		},
	}

	if err := ValidateMounts(config); err != nil {
		t.Errorf("Expected no error for similar names, got: %v", err)
	}
}
