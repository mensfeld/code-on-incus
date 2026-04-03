package session

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateMounts checks for nested container paths.
// Nesting is allowed when the child mount is readonly (e.g., mounting
// ~/.claude/skills as readonly inside a writable ~/.claude parent).
func ValidateMounts(config *MountConfig) error {
	if config == nil || len(config.Mounts) == 0 {
		return nil
	}

	// Check all pairs for nesting
	for i := 0; i < len(config.Mounts); i++ {
		for j := i + 1; j < len(config.Mounts); j++ {
			pathI := filepath.Clean(config.Mounts[i].ContainerPath)
			pathJ := filepath.Clean(config.Mounts[j].ContainerPath)

			if !isNestedPath(pathI, pathJ) {
				continue
			}

			// Determine which is the child (nested inside the other)
			childReadonly := childMountIsReadonly(
				pathI, config.Mounts[i].Readonly,
				pathJ, config.Mounts[j].Readonly,
			)

			if !childReadonly {
				return fmt.Errorf(
					"nested mount paths detected: '%s' and '%s' conflict (nested writable mounts are not allowed; set readonly = true on the child mount)",
					pathI, pathJ,
				)
			}
		}
	}

	return nil
}

// childMountIsReadonly returns true if the child (more deeply nested) path
// in a nested pair is readonly. For exact duplicates, both must be readonly.
func childMountIsReadonly(pathA string, readonlyA bool, pathB string, readonlyB bool) bool {
	cleanA := pathA + string(filepath.Separator)
	cleanB := pathB + string(filepath.Separator)

	if pathA == pathB {
		// Exact duplicates: both must be readonly
		return readonlyA && readonlyB
	}

	// pathA is the child (nested inside pathB)
	if strings.HasPrefix(cleanA, cleanB) {
		return readonlyA
	}

	// pathB is the child (nested inside pathA)
	return readonlyB
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
