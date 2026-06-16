package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AuditLog manages persistent logging of monitoring events
type AuditLog struct {
	file *os.File
	mu   sync.Mutex
}

// NewAuditLog creates a new audit log
func NewAuditLog(path string) (*AuditLog, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	// Open log file in append mode
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}

	return &AuditLog{
		file: file,
	}, nil
}

// WriteSnapshot writes a monitoring snapshot to the audit log
func (a *AuditLog) WriteSnapshot(snapshot MonitorSnapshot) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write as JSON Lines format (one JSON object per line)
	if _, err := a.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	return a.file.Sync()
}

// WriteThreat writes a threat event to the audit log
func (a *AuditLog) WriteThreat(threat ThreatEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := json.Marshal(threat)
	if err != nil {
		return fmt.Errorf("failed to marshal threat: %w", err)
	}

	// Write as JSON Lines format
	if _, err := a.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write threat: %w", err)
	}

	return a.file.Sync()
}

// Close closes the audit log file
func (a *AuditLog) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.file != nil {
		return a.file.Close()
	}

	return nil
}
