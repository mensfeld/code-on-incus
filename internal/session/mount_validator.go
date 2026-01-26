package session

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateMounts checks for nested container paths
func ValidateMounts(config *MountConfig) error {
	if config == nil || len(config.Mounts) == 0 {
		return nil
	}

	paths := make([]string, len(config.Mounts))
	for i, m := range config.Mounts {
		paths[i] = filepath.Clean(m.ContainerPath)
	}

	// Check all pairs for nesting
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if isNestedPath(paths[i], paths[j]) {
				return fmt.Errorf(
					"nested mount paths detected: '%s' and '%s' conflict",
					paths[i], paths[j],
				)
			}
		}
	}

	return nil
}

// isNestedPath returns true if pathA is nested inside pathB or vice versa
func isNestedPath(pathA, pathB string) bool {
	cleanA := filepath.Clean(pathA)
	cleanB := filepath.Clean(pathB)

	// Exact match
	if cleanA == cleanB {
		return true
	}

	// Check if one is prefix of other
	pathA = cleanA + string(filepath.Separator)
	pathB = cleanB + string(filepath.Separator)

	return strings.HasPrefix(pathA, pathB) || strings.HasPrefix(pathB, pathA)
}
