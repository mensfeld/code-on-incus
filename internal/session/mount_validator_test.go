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

func TestValidateMounts_ReadonlyField(t *testing.T) {
	config := &MountConfig{
		Mounts: []MountEntry{
			{ContainerPath: "/data1", Readonly: true},
			{ContainerPath: "/data2", Readonly: false},
			{ContainerPath: "/app"},
		},
	}

	if err := ValidateMounts(config); err != nil {
		t.Errorf("Expected no error for mounts with readonly field, got: %v", err)
	}

	// Verify readonly field is set correctly
	if !config.Mounts[0].Readonly {
		t.Error("Expected first mount to be readonly")
	}
	if config.Mounts[1].Readonly {
		t.Error("Expected second mount to NOT be readonly")
	}
	if config.Mounts[2].Readonly {
		t.Error("Expected third mount to NOT be readonly (default)")
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
