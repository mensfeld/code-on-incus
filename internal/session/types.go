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

// CredentialEntry represents a single host file to copy into a container: a
// host path pushed to a container path, chowned to the container's code
// user, and chmod'd to Mode if set. Expanded either from a named catalog
// bundle (see internal/tool/credentials) or an ad-hoc profile entry.
type CredentialEntry struct {
	HostPath string // Absolute path on host (expanded)
	// ContainerPath is either absolute (ad-hoc entries — used as-is, exactly
	// like MountEntry.ContainerPath) or relative to the container's home
	// directory (bundle entries — resolved by setupCredentials at apply
	// time, since the home directory, e.g. /root vs /home/code, isn't known
	// until the container exists).
	ContainerPath string
	Mode          string // Optional chmod mode, e.g. "0600"; "" = leave as pushed

	// BundleName is set when this entry was expanded from a named catalog
	// bundle. Bundle-sourced entries are never gated by trust — see
	// Untrusted below.
	BundleName string

	// Untrusted is true when this entry came from an untrusted (project-scope)
	// config file AND is an ad-hoc entry (BundleName == ""). SourcePath is
	// that file's absolute path. Used to gate ad-hoc credential entries whose
	// source config isn't trusted, behind `coi trust` — mirroring
	// SocketEntry, since a credential file (like a forwarded socket) has no
	// "within workspace" notion that would make it safe to leave ungated.
	Untrusted  bool
	SourcePath string
}

// CredentialConfig holds all credential entries for a session.
type CredentialConfig struct {
	Entries []CredentialEntry
}
