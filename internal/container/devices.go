package container

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DiskDeviceSources returns the host source paths of the container's
// instance-level disk devices, plus whether any of those disk devices still
// carries shift=true (the coi/pre-#689 creation-time signature).
//
// It shells `incus config device show <name>` ONCE and hands the YAML to the
// pure parseDiskDeviceSources. Non-expanded output is intentional: it lists
// only the coi-added mount devices (workspace, git-worktree-common, [[mount]]),
// not the profile-inherited root disk — exactly the source set the reuse path
// feeds to the shift decision via MountSources. Returns nil,false on any error.
func DiskDeviceSources(containerName string) (sources []string, hasShiftDevice bool) {
	out, err := IncusOutput("config", "device", "show", containerName)
	if err != nil {
		return nil, false
	}
	return parseDiskDeviceSources(out)
}

// parseDiskDeviceSources extracts, from `incus config device show` YAML, the
// host source paths of every type=disk device and whether any disk device has
// shift=true. Pure/text-only so it is unit-testable without incus. Device names
// are visited in sorted order so the returned sources are deterministic (Go map
// iteration is random); FirstBlockingSource is order-independent, but tests and
// log output are not.
func parseDiskDeviceSources(yamlOut string) (sources []string, hasShiftDevice bool) {
	var devices map[string]struct {
		Type   string `yaml:"type"`
		Source string `yaml:"source"`
		Shift  string `yaml:"shift"`
	}
	if err := yaml.Unmarshal([]byte(yamlOut), &devices); err != nil {
		return nil, false
	}
	names := make([]string, 0, len(devices))
	for name := range devices {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d := devices[name]
		if d.Type != "disk" {
			continue
		}
		if strings.TrimSpace(d.Source) != "" {
			sources = append(sources, d.Source)
		}
		if strings.TrimSpace(d.Shift) == "true" {
			hasShiftDevice = true
		}
	}
	return sources, hasShiftDevice
}
