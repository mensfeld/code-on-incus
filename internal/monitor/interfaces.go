package monitor

// MonitorDaemon is the interface implemented by *Daemon.
// Callers should accept this interface rather than the concrete *Daemon type
// so they can substitute a no-op stub in tests.
type MonitorDaemon interface {
	Stop() error
}

// Compile-time assertion: *Daemon must satisfy MonitorDaemon.
var _ MonitorDaemon = (*Daemon)(nil)
