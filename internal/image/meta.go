package image

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const metaFileName = "image-meta.json"

// BuildMeta records when a coi image was last built and which image it was built on top of.
type BuildMeta struct {
	BuiltAt   time.Time `json:"built_at"`
	BaseImage string    `json:"base_image"`
}

// StaleBaseResult is the outcome of a staleness check.
type StaleBaseResult struct {
	ChildAlias   string
	BaseAlias    string
	ChildBuiltAt time.Time
	BaseBuiltAt  time.Time
}

// RecordBuild writes a build timestamp entry for alias into coiDir/image-meta.json.
// Errors are silently ignored so a metadata write failure never blocks a build.
func RecordBuild(coiDir, alias, baseImage string) {
	path := filepath.Join(coiDir, metaFileName)
	m := loadMeta(path)
	m[alias] = BuildMeta{
		BuiltAt:   time.Now().UTC(),
		BaseImage: baseImage,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// CheckStaleBase reports whether childAlias's recorded base was rebuilt more
// recently than childAlias itself. Returns (result, true) when the check can be
// performed; returns (zero, false) when there is not enough metadata to decide.
func CheckStaleBase(coiDir, childAlias string) (StaleBaseResult, bool) {
	path := filepath.Join(coiDir, metaFileName)
	m := loadMeta(path)

	child, ok := m[childAlias]
	if !ok || child.BaseImage == "" {
		return StaleBaseResult{}, false
	}

	base, ok := m[child.BaseImage]
	if !ok {
		return StaleBaseResult{}, false
	}

	if !base.BuiltAt.After(child.BuiltAt) {
		return StaleBaseResult{}, false
	}

	return StaleBaseResult{
		ChildAlias:   childAlias,
		BaseAlias:    child.BaseImage,
		ChildBuiltAt: child.BuiltAt,
		BaseBuiltAt:  base.BuiltAt,
	}, true
}

func loadMeta(path string) map[string]BuildMeta {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]BuildMeta{}
	}
	var m map[string]BuildMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]BuildMeta{}
	}
	return m
}
