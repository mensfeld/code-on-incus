package session

// MountEntry represents a single directory mount at runtime
type MountEntry struct {
	HostPath      string // Absolute path on host (expanded)
	ContainerPath string // Absolute path in container
	DeviceName    string // Unique device name for Incus
	UseShift      bool   // Whether to use UID shifting
	Readonly      bool   // Mount read-only

	// Untrusted is true when this mount came from an untrusted (project-scope)
	// config file. SourcePath is that file's absolute path. Used to gate mounts
	// whose host path escapes the workspace behind explicit trust (`coi trust`).
	Untrusted  bool
	SourcePath string
}

// MountConfig holds all mount configurations for a session
type MountConfig struct {
	Mounts []MountEntry
}
