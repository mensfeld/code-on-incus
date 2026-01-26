package session

// MountEntry represents a single directory mount at runtime
type MountEntry struct {
	HostPath      string // Absolute path on host (expanded)
	ContainerPath string // Absolute path in container
	DeviceName    string // Unique device name for Incus
	UseShift      bool   // Whether to use UID shifting
}

// MountConfig holds all mount configurations for a session
type MountConfig struct {
	Mounts []MountEntry
}
