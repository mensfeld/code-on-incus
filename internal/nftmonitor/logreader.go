//go:build linux
// +build linux

package nftmonitor

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LogReader reads and parses nftables logs from journald
type LogReader struct {
	config      *Config
	journal     *JournalReader
	eventChan   chan *NetworkEvent
	journalChan chan string
}

// NewLogReader creates a new log reader
func NewLogReader(cfg *Config) (*LogReader, error) {
	journal, err := NewJournalReader()
	if err != nil {
		return nil, fmt.Errorf("failed to create journal reader: %w", err)
	}

	return &LogReader{
		config:      cfg,
		journal:     journal,
		eventChan:   make(chan *NetworkEvent, 100),
		journalChan: make(chan string, 100),
	}, nil
}

// Start begins reading and parsing logs
func (lr *LogReader) Start(ctx context.Context) error {
	// Start journal streaming in background
	go func() {
		if err := lr.journal.StreamLogs(ctx, lr.journalChan); err != nil {
			if ctx.Err() == nil {
				fmt.Printf("Journal streaming error: %v\n", err)
			}
		}
	}()

	// Parse incoming log messages
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-lr.journalChan:
			if event := lr.parseNFTLog(msg); event != nil {
				// Filter to only this container's IP
				// Check both the prefix IP and the actual SRC IP match
				if event.ContainerIP == lr.config.ContainerIP &&
					event.SrcIP == lr.config.ContainerIP {
					select {
					case lr.eventChan <- event:
					case <-ctx.Done():
						return ctx.Err()
					default:
						// Channel full, skip
					}
				}
			}
		}
	}
}

// Events returns the channel for receiving parsed network events
func (lr *LogReader) Events() <-chan *NetworkEvent {
	return lr.eventChan
}

// Close closes the log reader
func (lr *LogReader) Close() error {
	close(lr.eventChan)
	close(lr.journalChan)
	return lr.journal.Close()
}

// parseNFTLog parses a nftables kernel log message into a NetworkEvent
// Example log: "NFT_COI[10.47.62.50]: IN=incusbr0 OUT=eth0 SRC=10.47.62.50 DST=8.8.8.8 PROTO=TCP SPT=54321 DPT=53 SYN"
func (lr *LogReader) parseNFTLog(line string) *NetworkEvent {
	// Check for our log prefixes
	var containerIP string
	if strings.Contains(line, "NFT_COI[") {
		containerIP = extractIPFromPrefix(line, "NFT_COI[")
	} else if strings.Contains(line, "NFT_DNS[") {
		containerIP = extractIPFromPrefix(line, "NFT_DNS[")
	} else if strings.Contains(line, "NFT_SUSPICIOUS[") {
		containerIP = extractIPFromPrefix(line, "NFT_SUSPICIOUS[")
	} else {
		return nil
	}

	if containerIP == "" {
		return nil
	}

	// Extract fields
	event := &NetworkEvent{
		Timestamp:   time.Now(),
		ContainerIP: containerIP,
		SrcIP:       extractField(line, "SRC="),
		DstIP:       extractField(line, "DST="),
		Protocol:    extractField(line, "PROTO="),
		Interface:   extractField(line, "IN=") + "/" + extractField(line, "OUT="),
	}

	// Extract ports
	if spt := extractField(line, "SPT="); spt != "" {
		if port, err := strconv.Atoi(spt); err == nil {
			event.SrcPort = port
		}
	}
	if dpt := extractField(line, "DPT="); dpt != "" {
		if port, err := strconv.Atoi(dpt); err == nil {
			event.DstPort = port
		}
	}

	// Extract TCP flags
	event.Flags = extractTCPFlags(line)

	return event
}

// extractIPFromPrefix extracts IP from prefix like "NFT_COI[10.47.62.50]"
func extractIPFromPrefix(line, prefix string) string {
	start := strings.Index(line, prefix)
	if start == -1 {
		return ""
	}
	start += len(prefix)
	end := strings.Index(line[start:], "]")
	if end == -1 {
		return ""
	}
	return line[start : start+end]
}

// extractField extracts a field value from log line
// Example: "SRC=10.47.62.50" -> "10.47.62.50"
func extractField(line, field string) string {
	re := regexp.MustCompile(field + `([^\s]+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractTCPFlags extracts TCP flags from log line
func extractTCPFlags(line string) string {
	flags := []string{}
	if strings.Contains(line, " SYN ") || strings.HasSuffix(line, " SYN") {
		flags = append(flags, "SYN")
	}
	if strings.Contains(line, " ACK ") || strings.HasSuffix(line, " ACK") {
		flags = append(flags, "ACK")
	}
	if strings.Contains(line, " FIN ") || strings.HasSuffix(line, " FIN") {
		flags = append(flags, "FIN")
	}
	if strings.Contains(line, " RST ") || strings.HasSuffix(line, " RST") {
		flags = append(flags, "RST")
	}
	if strings.Contains(line, " PSH ") || strings.HasSuffix(line, " PSH") {
		flags = append(flags, "PSH")
	}
	return strings.Join(flags, ",")
}
