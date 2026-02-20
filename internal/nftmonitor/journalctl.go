//go:build linux
// +build linux

package nftmonitor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
)

// Debug controls whether debug output is enabled
// Set COI_NFT_DEBUG=1 to enable
var Debug = os.Getenv("COI_NFT_DEBUG") == "1"

func debugf(format string, args ...interface{}) {
	if Debug {
		fmt.Fprintf(os.Stderr, "[NFT-DEBUG] "+format+"\n", args...)
	}
}

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

	// Skip past the current tail entry so Next() reads truly NEW entries
	// Without this, the first Wait()+Next() might read the last existing entry
	// instead of waiting for new ones
	_, _ = journal.Next()

	debugf("JournalReader initialized, waiting for new kernel log entries")

	return &JournalReader{journal: journal}, nil
}

// StreamLogs streams kernel log messages to the provided channel
func (jr *JournalReader) StreamLogs(ctx context.Context, msgChan chan<- string) error {
	// Close journal when we're done streaming
	defer jr.Close()

	debugf("StreamLogs started, entering main loop")

	for {
		select {
		case <-ctx.Done():
			debugf("Context done, exiting StreamLogs")
			return ctx.Err()
		default:
		}

		// Check if we've been closed
		jr.mu.Lock()
		if jr.closed {
			jr.mu.Unlock()
			debugf("Journal closed, exiting StreamLogs")
			return nil
		}
		journal := jr.journal
		jr.mu.Unlock()

		if journal == nil {
			debugf("Journal is nil, exiting StreamLogs")
			return nil
		}

		// Wait for new journal entries (with timeout)
		// Returns: SD_JOURNAL_NOP (0) = nothing, SD_JOURNAL_APPEND (1) = new entries
		waitResult := journal.Wait(time.Second)
		if waitResult == sdjournal.SD_JOURNAL_APPEND {
			debugf("Journal.Wait returned APPEND - new entries available")
		}

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
			// Note: GetData returns "MESSAGE=<content>", we need to strip the prefix
			msg, err := jr.journal.GetData("MESSAGE")
			jr.mu.Unlock()

			if err != nil {
				debugf("GetData(MESSAGE) failed: %v", err)
				continue // Skip entries without MESSAGE
			}

			// Strip the "MESSAGE=" prefix that GetData returns
			msg = strings.TrimPrefix(msg, "MESSAGE=")

			debugf("Got journal message: %.100s...", msg)

			// Send to channel (non-blocking)
			select {
			case msgChan <- msg:
				debugf("Message sent to channel")
			case <-ctx.Done():
				return ctx.Err()
			default:
				debugf("Channel full, skipping message")
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
