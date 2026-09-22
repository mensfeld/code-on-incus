package container

import (
	"fmt"
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
	// Decode device fields as `any`, not `string`: Incus renders device config
	// values as quoted strings today (shift: "true"), but a version that emitted
	// an unquoted scalar (shift: true) would fail a `string`-typed unmarshal and
	// silently disable the whole heal. scalarString coerces either shape.
	var devices map[string]map[string]any
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
		if scalarString(d["type"]) != "disk" {
			continue
		}
		if src := strings.TrimSpace(scalarString(d["source"])); src != "" {
			sources = append(sources, src)
		}
		if strings.TrimSpace(scalarString(d["shift"])) == "true" {
			hasShiftDevice = true
		}
	}
	return sources, hasShiftDevice
}

// DiskDeviceShiftBySource returns, for each instance-level disk device, whether
// it carries shift=true, keyed by the device's host source path. It lets the
// reuse path detect when a persistent container's attached mount devices no
// longer match the shift the current config asks for (#604) — matching by
// source path is stable across device-index changes (mount-0, mount-1, …).
//
// Shells `incus config device show` ONCE and hands the YAML to the pure
// parseDiskDeviceShiftBySource. Returns nil on any error.
func DiskDeviceShiftBySource(containerName string) (map[string]bool, error) {
	out, err := IncusOutput("config", "device", "show", containerName)
	if err != nil {
		return nil, err
	}
	return parseDiskDeviceShiftBySource(out), nil
}

// parseDiskDeviceShiftBySource maps each type=disk device's host source path to
// its shift flag. Pure/text-only so it is unit-testable without incus. A device
// with an empty source is skipped. When two devices share a source, the last in
// sorted-name order wins (a benign edge; the same source rarely maps twice).
func parseDiskDeviceShiftBySource(yamlOut string) map[string]bool {
	var devices map[string]map[string]any
	if err := yaml.Unmarshal([]byte(yamlOut), &devices); err != nil {
		return nil
	}
	names := make([]string, 0, len(devices))
	for name := range devices {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]bool, len(names))
	for _, name := range names {
		d := devices[name]
		if scalarString(d["type"]) != "disk" {
			continue
		}
		src := strings.TrimSpace(scalarString(d["source"]))
		if src == "" {
			continue
		}
		out[src] = strings.TrimSpace(scalarString(d["shift"])) == "true"
	}
	return out
}

// scalarString renders a scalar YAML value (string/bool/int) as a string, so a
// device field survives whether Incus emits it quoted or unquoted. Returns ""
// for nil or a non-scalar value.
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case map[string]any, []any:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
