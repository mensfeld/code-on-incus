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

// SocketEntry represents a host unix socket forwarded into the container at
// runtime (via an Incus proxy device). It generalizes SSH agent forwarding.
type SocketEntry struct {
	HostPath      string // Absolute host socket path (expanded)
	ContainerPath string // In-container socket path
	EnvVar        string // Optional env var NAME set to ContainerPath ("" = none)
	DeviceName    string // Unique Incus proxy-device name

	// Untrusted is true when this entry came from an untrusted (project-scope)
	// config file. SourcePath is that file's absolute path. Untrusted sockets are
	// gated behind explicit trust (`coi trust`).
	Untrusted  bool
	SourcePath string
}

// SocketConfig holds all forwarded-socket entries for a session.
type SocketConfig struct {
	Sockets []SocketEntry
}
