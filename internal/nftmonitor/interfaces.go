package nftmonitor

// NFTMonitorDaemon is the interface implemented by *Daemon.
// Callers should accept this interface rather than the concrete *Daemon type
// so they can substitute a no-op stub in tests.
type NFTMonitorDaemon interface {
	Stop() error
}

// Compile-time assertion: *Daemon must satisfy NFTMonitorDaemon.
var _ NFTMonitorDaemon = (*Daemon)(nil)
