package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var logsFollow bool

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Stream log output in real time")
	rootCmd.AddCommand(logsCmd)
}

var logsCmd = &cobra.Command{
	Use:   "logs [container]",
	Short: "Show session logs for a container",
	Long: `Show the session logs written by coi during a shell session.

Informational messages are read from ~/.coi/logs/<container>.stdout.log.
Warnings and errors are read from ~/.coi/logs/<container>.stderr.log (prefixed with [ERR]).

With --follow / -f the command tails both files in real time until Ctrl+C.

Examples:
  coi logs                     # Auto-detect container, print logs
  coi logs coi-abc-1           # Specific container, print logs
  coi logs coi-abc-1 -f        # Tail logs live`,
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	containerName, err := resolveMonitorContainer(args)
	if err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	outPath := filepath.Join(homeDir, ".coi", "logs", containerName+".stdout.log")
	errPath := filepath.Join(homeDir, ".coi", "logs", containerName+".stderr.log")

	if !logsFollow {
		return dumpLogs(outPath, errPath)
	}
	return tailLogs(ctx, outPath, errPath)
}

// dumpLogs prints both log files once, interleaving by modification-time order.
// Simpler approach: print stdout.log, then stderr.log lines prefixed with [ERR].
func dumpLogs(outPath, errPath string) error {
	anyOutput := false

	if err := printLogFile(os.Stdout, outPath, ""); err != nil && !os.IsNotExist(err) {
		return err
	} else if err == nil {
		anyOutput = true
	}

	if err := printLogFile(os.Stdout, errPath, "[ERR] "); err != nil && !os.IsNotExist(err) {
		return err
	} else if err == nil {
		anyOutput = true
	}

	if !anyOutput {
		return fmt.Errorf("no logs found for container (expected %s or %s)", outPath, errPath)
	}
	return nil
}

// printLogFile reads and prints a log file line by line with an optional prefix.
func printLogFile(w io.Writer, path, prefix string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Fprintf(w, "%s%s\n", prefix, scanner.Text())
	}
	return scanner.Err()
}

// tailLogs follows both log files simultaneously until ctx is cancelled.
// Uses poll-based tailing: seek to EOF, then repeatedly read new bytes.
func tailLogs(ctx interface{ Done() <-chan struct{} }, outPath, errPath string) error {
	outFile, outOffset, err := openForTail(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[logs] warning: cannot open %s: %v\n", outPath, err)
	}
	errFile, errOffset, err := openForTail(errPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[logs] warning: cannot open %s: %v\n", errPath, err)
	}

	if outFile != nil {
		defer outFile.Close()
	}
	if errFile != nil {
		defer errFile.Close()
	}

	fmt.Fprintf(os.Stderr, "[logs] tailing logs (Ctrl+C to stop)\n")

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if outFile != nil {
				outOffset = readNewLines(outFile, outOffset, os.Stdout, "")
			} else {
				// Retry opening if it didn't exist yet
				if f, off, err := openForTail(outPath); err == nil {
					outFile = f
					outOffset = off
				}
			}
			if errFile != nil {
				errOffset = readNewLines(errFile, errOffset, os.Stdout, "[ERR] ")
			} else {
				if f, off, err := openForTail(errPath); err == nil {
					errFile = f
					errOffset = off
				}
			}
		}
	}
}

// openForTail opens a file and seeks to EOF, returning the file and EOF offset.
func openForTail(path string) (*os.File, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, offset, nil
}

// readNewLines reads any bytes appended since offset, prints them line by line, and returns the new offset.
// The offset is only advanced for bytes that were successfully scanned; a scanner error (e.g. line too long)
// leaves the offset unchanged so the data is retried on the next tick rather than silently dropped.
func readNewLines(f *os.File, offset int64, w io.Writer, prefix string) int64 {
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset
	}
	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return offset
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MiB max line length
	scanned := int64(0)
	for scanner.Scan() {
		line := scanner.Bytes()
		scanned += int64(len(line)) + 1 // +1 for the newline consumed by the scanner
		fmt.Fprintf(w, "%s%s\n", prefix, line)
	}
	if scanner.Err() != nil {
		// Do not advance past unscanned data; it will be retried on the next tick.
		return offset + scanned
	}
	return offset + int64(len(data))
}
