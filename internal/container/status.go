package container

import "strings"

// Incus reports a container's lifecycle state as a string. The JSON API returns
// title case ("Running", "Stopped", "Frozen"), while some `incus list` output
// formats report upper case ("RUNNING", "STOPPED"). Comparing case-sensitively
// therefore silently misses states depending on which surface produced the
// value — the reason scattered call sites diverged between `== "Running"` and
// `strings.EqualFold`. Always compare through these helpers.

// StatusIsRunning reports whether an Incus status string means "running",
// case-insensitively.
func StatusIsRunning(status string) bool { return strings.EqualFold(status, "running") }

// StatusIsStopped reports whether an Incus status string means "stopped",
// case-insensitively.
func StatusIsStopped(status string) bool { return strings.EqualFold(status, "stopped") }

// StatusIsFrozen reports whether an Incus status string means "frozen" (paused),
// case-insensitively.
func StatusIsFrozen(status string) bool { return strings.EqualFold(status, "frozen") }
