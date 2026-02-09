//go:build linux
// +build linux

package nftmonitor

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
)

// JournalReader wraps systemd journal for reading kernel logs
type JournalReader struct {
	journal *sdjournal.Journal
}

// NewJournalReader creates a new journal reader for kernel logs
func NewJournalReader() (*JournalReader, error) {
	journal, err := sdjournal.NewJournal()
	if err != nil {
		return nil, fmt.Errorf("failed to open systemd journal: %w", err)
	}

	// Filter to kernel messages only
	if err := journal.AddMatch("_TRANSPORT=kernel"); err != nil {
		journal.Close()
		return nil, fmt.Errorf("failed to add kernel filter: %w", err)
	}

	// Start from the end (only read new logs)
	if err := journal.SeekTail(); err != nil {
		journal.Close()
		return nil, fmt.Errorf("failed to seek to end of journal: %w", err)
	}

	return &JournalReader{journal: journal}, nil
}

// StreamLogs streams kernel log messages to the provided channel
func (jr *JournalReader) StreamLogs(ctx context.Context, msgChan chan<- string) error {
	defer jr.journal.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Wait for new journal entries (with timeout)
		jr.journal.Wait(time.Second)

		// Read all available entries
		for {
			n, err := jr.journal.Next()
			if err != nil {
				return fmt.Errorf("failed to read next journal entry: %w", err)
			}
			if n == 0 {
				break // No more entries
			}

			// Get the MESSAGE field (kernel log message)
			msg, err := jr.journal.GetData("MESSAGE")
			if err != nil {
				continue // Skip entries without MESSAGE
			}

			// Send to channel (non-blocking)
			select {
			case msgChan <- msg:
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Channel full, skip this message
			}
		}
	}
}

// Close closes the journal reader
func (jr *JournalReader) Close() error {
	if jr.journal != nil {
		return jr.journal.Close()
	}
	return nil
}
