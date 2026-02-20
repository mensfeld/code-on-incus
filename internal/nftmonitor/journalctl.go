//go:build linux
// +build linux

package nftmonitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
)

// JournalReader wraps systemd journal for reading kernel logs
type JournalReader struct {
	journal   *sdjournal.Journal
	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
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
	// Close journal when we're done streaming
	defer jr.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if we've been closed
		jr.mu.Lock()
		if jr.closed {
			jr.mu.Unlock()
			return nil
		}
		journal := jr.journal
		jr.mu.Unlock()

		if journal == nil {
			return nil
		}

		// Wait for new journal entries (with timeout)
		journal.Wait(time.Second)

		// Check again after wait
		jr.mu.Lock()
		if jr.closed {
			jr.mu.Unlock()
			return nil
		}
		jr.mu.Unlock()

		// Read all available entries
		for {
			jr.mu.Lock()
			if jr.closed || jr.journal == nil {
				jr.mu.Unlock()
				return nil
			}
			n, err := jr.journal.Next()
			jr.mu.Unlock()

			if err != nil {
				return fmt.Errorf("failed to read next journal entry: %w", err)
			}
			if n == 0 {
				break // No more entries
			}

			jr.mu.Lock()
			if jr.closed || jr.journal == nil {
				jr.mu.Unlock()
				return nil
			}
			// Get the MESSAGE field (kernel log message)
			msg, err := jr.journal.GetData("MESSAGE")
			jr.mu.Unlock()

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

// Close closes the journal reader (safe to call multiple times)
func (jr *JournalReader) Close() error {
	var closeErr error
	jr.closeOnce.Do(func() {
		jr.mu.Lock()
		defer jr.mu.Unlock()
		jr.closed = true
		if jr.journal != nil {
			closeErr = jr.journal.Close()
			jr.journal = nil
		}
	})
	return closeErr
}
