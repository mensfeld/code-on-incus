package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mensfeld/code-on-incus/internal/audit"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/spf13/cobra"
)

var (
	auditFollow bool
	auditFile   string
)

func init() {
	auditCmd.Flags().BoolVarP(&auditFollow, "follow", "f", false, "Stream events live from the container's in-sandbox collector")
	auditCmd.Flags().StringVar(&auditFile, "file", "", "Read from a specific audit JSONL file instead of the host-side default path")
	rootCmd.AddCommand(auditCmd)
}

var auditCmd = &cobra.Command{
	Use:   "audit [container]",
	Short: "Stream threat-event audit records from a sandbox session",
	Long: `Stream threat-event audit records from a sandbox session as JSON lines.

Default mode reads the host-side audit log written by the security monitor
daemon (~/.coi/audit/<container>.jsonl). With --follow, the command spawns an
in-container collector that emits live events from auditd (when present),
syslog/auth.log, periodic ss snapshots, and periodic ps snapshots.

Event shape (one JSON object per line):
  {"ts":"...","sessionId":"...","container":"...","type":"exec|net|file|audit",
   "pid":...,"comm":"...","args":"...","peer":"...","path":"...","msg":"...",...}

Examples:
  coi audit                           # Auto-detect container, dump host-side log
  coi audit coi-abc-1                 # Specific container, dump host-side log
  coi audit coi-abc-1 --follow        # Live in-container collector, JSONL on stdout
  coi audit --file ./session.jsonl    # Read from an arbitrary JSONL file`,
	RunE: runAudit,
}

func runAudit(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --file overrides every other source.
	if auditFile != "" {
		return audit.HostAuditTail(auditFile, auditFollow, "", "", os.Stdout)
	}

	containerName, err := resolveMonitorContainer(args)
	if err != nil {
		return err
	}

	if !auditFollow {
		return runAuditDump(containerName)
	}
	return runAuditFollow(ctx, containerName)
}

// runAuditDump prints the existing host-side audit log for a container.
func runAuditDump(containerName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}
	path := filepath.Join(homeDir, ".coi", "audit", containerName+".jsonl")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("no audit log found for container %s (start a session with monitoring enabled, or use --follow for live in-container events)", containerName)
	}
	return audit.HostAuditTail(path, false, containerName, containerName, os.Stdout)
}

// runAuditFollow pushes the agent script into the container and streams its
// stdout, decorating each event with the container name as sessionId.
func runAuditFollow(ctx context.Context, containerName string) error {
	mgr := container.NewManager(containerName)
	running, err := mgr.Running()
	if err != nil {
		return fmt.Errorf("check container: %w", err)
	}
	if !running {
		return fmt.Errorf("container %s is not running; --follow needs a live container", containerName)
	}

	// Write the embedded agent to a tempfile, push it into the container,
	// then `incus exec` it. We push to /tmp because /var may be read-only
	// on some hardened profiles.
	localPath, err := audit.WriteAgentScriptTemp()
	if err != nil {
		return fmt.Errorf("stage agent: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(localPath))

	remotePath := "/tmp/coi-audit-collector.sh"
	if err := mgr.PushFile(localPath, remotePath); err != nil {
		return fmt.Errorf("push agent into container: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[audit] streaming from %s (Ctrl+C to stop)\n", containerName)

	// Build the incus exec command directly so we can pipe its stdout.
	// We deliberately don't use container.Manager.Exec — that drives stdout
	// to os.Stderr; we need a pipe.
	incusArgs := []string{"exec", containerName, "--", "sh", remotePath}
	cmd, stdout, err := newIncusPipedCommand(ctx, incusArgs)
	if err != nil {
		return fmt.Errorf("build agent command: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	collector := &audit.Collector{
		SessionID: containerName,
		Container: containerName,
		Out:       os.Stdout,
		OnError: func(err error) {
			fmt.Fprintf(os.Stderr, "[audit] parse: %v\n", err)
		},
	}
	streamErr := collector.Run(ctx, stdout)

	// Wait for the agent to exit cleanly.
	_ = cmd.Wait()

	if ctx.Err() != nil {
		return nil
	}
	return streamErr
}

// newIncusPipedCommand returns an exec.Cmd ready to run an incus subcommand
// (mirroring the sg-or-direct logic in internal/container/commands.go) with
// its stdout piped back to the caller.
func newIncusPipedCommand(ctx context.Context, args []string) (*exec.Cmd, io.ReadCloser, error) {
	// Reuse the package's own command builder via a thin shell-out: this
	// keeps us aligned with however that file evolves (sg vs direct).
	cmdArgs := []string{}
	cmdArgs = append(cmdArgs, "incus")
	cmdArgs = append(cmdArgs, "--project", container.IncusProject)
	cmdArgs = append(cmdArgs, args...)

	var cmd *exec.Cmd
	if container.CanUseSg() {
		joined := joinShellArgs(cmdArgs)
		cmd = exec.CommandContext(ctx, "sg", container.IncusGroup, "-c", joined)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", joinShellArgs(cmdArgs))
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

// joinShellArgs single-quotes args for safe POSIX-shell concatenation.
func joinShellArgs(args []string) string {
	out := make([]byte, 0, 64)
	for i, a := range args {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, '\'')
		for j := 0; j < len(a); j++ {
			if a[j] == '\'' {
				out = append(out, []byte(`'\''`)...)
				continue
			}
			out = append(out, a[j])
		}
		out = append(out, '\'')
	}
	return string(out)
}
