package monitor

import "time"

// ThreatLevel indicates severity of detected threat
type ThreatLevel string

const (
	ThreatLevelInfo     ThreatLevel = "info"
	ThreatLevelWarning  ThreatLevel = "warning"
	ThreatLevelHigh     ThreatLevel = "high"     // Auto-pause
	ThreatLevelCritical ThreatLevel = "critical" // Auto-kill
)

// ThreatEvent represents a detected security event
type ThreatEvent struct {
	ID          string      `json:"id"` // Unique event ID
	Timestamp   time.Time   `json:"timestamp"`
	Level       ThreatLevel `json:"level"`
	Category    string      `json:"category"`    // "network", "process", "filesystem", "env"
	Title       string      `json:"title"`       // "Reverse shell detected"
	Description string      `json:"description"` // Detailed explanation
	Evidence    interface{} `json:"evidence"`    // Supporting data (connection, process, etc.)
	Action      string      `json:"action"`      // "logged", "alerted", "paused", "killed"
}

// NetworkThreat represents suspicious network activity
type NetworkThreat struct {
	Connection Connection `json:"connection"`
	Reason     string     `json:"reason"`      // "Unexpected IP", "Reverse shell port", etc.
	RemoteHost string     `json:"remote_host"` // Resolved hostname if available
}

// ProcessThreat represents suspicious process activity
type ProcessThreat struct {
	PID        int      `json:"pid"`
	Command    string   `json:"command"` // Full command line
	User       string   `json:"user"`
	Pattern    string   `json:"pattern"`    // "nc -e", "bash -i", etc.
	Indicators []string `json:"indicators"` // List of suspicious patterns matched
}

// FilesystemThreat represents suspicious file access
type FilesystemThreat struct {
	ReadBytesMB float64 `json:"read_bytes_mb"`
	ReadRate    float64 `json:"read_rate_mb_per_sec"`
	FilesRead   int     `json:"files_read"`
	Duration    string  `json:"duration"`
	Threshold   float64 `json:"threshold_mb"`
}

// MonitorSnapshot represents a point-in-time view of container metrics
type MonitorSnapshot struct {
	Timestamp     time.Time       `json:"timestamp"`
	ContainerName string          `json:"container_name"`
	ContainerIP   string          `json:"container_ip,omitempty"`
	Network       NetworkStats    `json:"network"`
	Processes     ProcessStats    `json:"processes"`
	Filesystem    FilesystemStats `json:"filesystem"`
	Resources     ResourceStats   `json:"resources"`
	Threats       []ThreatEvent   `json:"threats"`
	Errors        []string        `json:"errors,omitempty"`
}

// ProcessStats holds process information
type ProcessStats struct {
	Available  bool      `json:"available"`
	TotalCount int       `json:"total_count"`
	Processes  []Process `json:"processes,omitempty"`
}

// Process represents a running process
type Process struct {
	PID       int    `json:"pid"`
	PPID      int    `json:"ppid"`
	User      string `json:"user"`
	Command   string `json:"command"`    // Full command line
	EnvAccess bool   `json:"env_access"` // Has accessed /proc/*/environ
}

// FilesystemStats holds workspace access statistics
type FilesystemStats struct {
	Available         bool    `json:"available"`
	WorkspacePath     string  `json:"workspace_path,omitempty"`
	TotalReadMB       float64 `json:"total_read_mb"`
	ReadRateMBPerSec  float64 `json:"read_rate_mb_per_sec"`
	TotalWriteMB      float64 `json:"total_write_mb"`
	WriteRateMBPerSec float64 `json:"write_rate_mb_per_sec"`
	FilesAccessed     int     `json:"files_accessed,omitempty"`
	// Disk space monitoring (for /tmp and /workspace)
	TmpUsedMB      float64 `json:"tmp_used_mb,omitempty"`
	TmpTotalMB     float64 `json:"tmp_total_mb,omitempty"`
	TmpUsedPercent float64 `json:"tmp_used_percent,omitempty"`
}

// NetworkStats represents network connection information
type NetworkStats struct {
	ActiveConnections int          `json:"active_connections"`
	Connections       []Connection `json:"connections,omitempty"`
	SuspiciousCount   int          `json:"suspicious_count"`
}

// Connection represents a network connection
type Connection struct {
	Protocol      string `json:"protocol"`
	LocalAddr     string `json:"local_addr"`
	RemoteAddr    string `json:"remote_addr"`
	State         string `json:"state"`
	UID           int    `json:"uid,omitempty"`
	Suspicious    bool   `json:"suspicious"`               // Flagged as suspicious
	SuspectReason string `json:"suspect_reason,omitempty"` // Why flagged
}

// ResourceStats represents container resource usage
type ResourceStats struct {
	CPUTimeSeconds float64 `json:"cpu_time_seconds"`
	UserCPUSeconds float64 `json:"user_cpu_seconds"`
	SysCPUSeconds  float64 `json:"sys_cpu_seconds"`
	MemoryMB       float64 `json:"memory_mb"`
	MemoryLimitMB  float64 `json:"memory_limit_mb,omitempty"`
	IOReadMB       float64 `json:"io_read_mb"`
	IOWriteMB      float64 `json:"io_write_mb"`
}

// DaemonConfig configures the monitoring daemon
type DaemonConfig struct {
	ContainerName  string
	WorkspacePath  string
	PollInterval   time.Duration
	AuditLogPath   string
	AllowedCIDRs   []string // CIDR ranges for allowed networks
	AllowedDomains []string // Domains from network allowlist

	// Threat detection thresholds
	FileReadThresholdMB   float64 // MB read in poll interval
	FileReadRateMBPerSec  float64 // MB/sec sustained rate
	FileWriteThresholdMB  float64 // MB written in poll interval
	FileWriteRateMBPerSec float64 // MB/sec sustained write rate

	// Response configuration
	AutoPauseOnHigh    bool
	AutoKillOnCritical bool

	// Callbacks
	OnThreat func(ThreatEvent)
	OnError  func(error)
	OnAction func(action, message string) // Called when container is paused/killed
}
