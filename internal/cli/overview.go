package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var overviewRefreshInterval int

var overviewCmd = &cobra.Command{
	Use:   "overview",
	Short: "Show a live table of running coi sessions",
	Long: `Show a continuously refreshing table of running coi sessions, the
container backing each one, the workspace it was launched against, and how
long it has been up.

Read-only: no container actions are taken from this command. The display
rerenders every --refresh-interval seconds (default: 2). Press Ctrl+C to
exit.

When stdout is not a terminal, the command renders a single snapshot and
returns instead of looping, so it stays usable from cron, scripts and CI.

Examples:
  coi overview
  coi overview --refresh-interval 5
`,
	RunE: overviewCommand,
}

func init() {
	overviewCmd.Flags().IntVar(&overviewRefreshInterval, "refresh-interval", 2,
		"Refresh interval in seconds (must be >= 1)")
}

// overviewSession is the projection of a running session shown in one row.
type overviewSession struct {
	ID        string
	Container string
	Workspace string
	Mode      string
	Started   time.Time
	Uptime    time.Duration
}

func overviewCommand(cmd *cobra.Command, args []string) error {
	if overviewRefreshInterval < 1 {
		return &ExitCodeError{
			Code:    2,
			Message: "--refresh-interval must be >= 1",
		}
	}

	// One-shot when stdout isn't a TTY: keeps the command useful in pipes,
	// CI sandboxes and journald without forcing a fake TTY.
	if !isTerminal(os.Stdout) {
		rows, header, err := collectOverview()
		if err != nil {
			return err
		}
		renderOverview(os.Stdout, header, rows, time.Now(), false)
		return nil
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	interval := time.Duration(overviewRefreshInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	draw := func() error {
		rows, header, err := collectOverview()
		if err != nil {
			return err
		}
		// \033[2J\033[H matches monitor --watch: clear screen + cursor home.
		fmt.Print("\033[2J\033[H")
		renderOverview(os.Stdout, header, rows, time.Now(), true)
		return nil
	}

	if err := draw(); err != nil {
		return err
	}

	for {
		select {
		case <-ticker.C:
			if err := draw(); err != nil {
				fmt.Fprintf(os.Stderr, "overview: %v\n", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// overviewHeader is what the top banner reports about the world right now.
type overviewHeader struct {
	Version    string
	Sessions   int
	Containers int
}

// collectOverview gathers running containers + their session metadata and
// returns a row per running coi session.
func collectOverview() ([]overviewSession, overviewHeader, error) {
	header := overviewHeader{Version: Version}

	toolInstance, err := getConfiguredTool(app.cfg)
	if err != nil {
		return nil, header, err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, header, fmt.Errorf("failed to get home directory: %w", err)
	}
	sessionsDir := session.GetSessionsDir(filepath.Join(homeDir, ".coi"), toolInstance)

	containers, err := listActiveContainers()
	if err != nil {
		return nil, header, fmt.Errorf("failed to list containers: %w", err)
	}

	// Map container -> metadata so we can label the workspace + mode and
	// recover the session id, which list.go already uses the same way.
	type meta struct {
		sessionID  string
		workspace  string
		persistent bool
	}
	byContainer := make(map[string]meta)
	if entries, err := os.ReadDir(sessionsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			metadataPath := filepath.Join(sessionsDir, entry.Name(), "metadata.json")
			data, err := os.ReadFile(metadataPath)
			if err != nil {
				continue
			}
			var m session.SessionMetadata
			if err := json.Unmarshal(data, &m); err != nil || m.ContainerName == "" {
				continue
			}
			byContainer[m.ContainerName] = meta{
				sessionID:  m.SessionID,
				workspace:  m.Workspace,
				persistent: m.Persistent,
			}
		}
	}

	now := time.Now()
	rows := make([]overviewSession, 0, len(containers))
	for _, c := range containers {
		// Only Running containers are interesting in an "overview"; match
		// what `incus list` reports as Status.
		if !strings.EqualFold(c.Status, "running") {
			continue
		}
		started, _ := time.ParseInLocation("2006-01-02 15:04:05", c.CreatedAt, time.Local)
		row := overviewSession{
			Container: c.Name,
			Started:   started,
		}
		if !started.IsZero() {
			row.Uptime = now.Sub(started)
		}
		if m, ok := byContainer[c.Name]; ok {
			row.ID = m.sessionID
			row.Workspace = m.workspace
			row.Mode = "ephemeral"
			if m.persistent {
				row.Mode = "persistent"
			}
		}
		rows = append(rows, row)
	}

	header.Containers = len(rows)
	header.Sessions = 0
	for _, r := range rows {
		if r.ID != "" {
			header.Sessions++
		}
	}
	return rows, header, nil
}

// renderOverview writes the overview to w. interactive controls whether the
// keybindings footer is shown: it's noise on a one-shot stdout dump.
func renderOverview(w io.Writer, header overviewHeader, rows []overviewSession, at time.Time, interactive bool) {
	fmt.Fprintf(w, "coi overview  v%s  sessions=%d  running=%d  %s\n",
		header.Version, header.Sessions, header.Containers,
		at.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w, strings.Repeat("-", 72))

	if len(rows) == 0 {
		fmt.Fprintln(w, "(no running sessions)")
	} else {
		tbl := NewTable("SESSION", "CONTAINER", "WORKSPACE", "MODE", "STARTED", "UPTIME")
		tbl.SetOutput(w)
		for _, r := range rows {
			tbl.AddRow(
				shortenID(r.ID),
				r.Container,
				shortenPath(r.Workspace, 32),
				r.Mode,
				formatStarted(r.Started),
				formatUptime(r.Uptime),
			)
		}
		tbl.Render()
	}

	if interactive {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "[Ctrl+C] quit   refresh=%ds\n", overviewRefreshInterval)
	}
}

// shortenID trims a session id to its first 12 chars for table display.
// Empty stays empty so we don't print bogus prefixes for orphan containers.
func shortenID(id string) string {
	if id == "" {
		return "-"
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// shortenPath trims long workspace paths from the left so the trailing
// directory (the part operators recognise) is preserved.
func shortenPath(p string, maxLen int) string {
	if p == "" {
		return "-"
	}
	if len(p) <= maxLen {
		return p
	}
	return "..." + p[len(p)-(maxLen-3):]
}

func formatStarted(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

// formatUptime renders a duration the way operators read uptime: 2d3h, 4h7m,
// 12m, 30s. Sub-second uptimes show as "<1s" rather than "0s" to make a
// freshly-started container obvious.
func formatUptime(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	if d < time.Second {
		return "<1s"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	d -= time.Duration(mins) * time.Minute
	secs := int(d / time.Second)

	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm%ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// isTerminal returns true when f is a real terminal. It uses the TIOCGWINSZ
// ioctl rather than os.ModeCharDevice because /dev/null is also a character
// device: ModeCharDevice would misreport `coi overview > /dev/null` as a TTY and
// trap it in the refresh loop instead of the one-shot path. This matches
// build_helpers.go's stdinIsTerminal.
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	return err == nil
}

// renderOverviewToString is a small helper used in tests.
func renderOverviewToString(header overviewHeader, rows []overviewSession, at time.Time, interactive bool) string {
	var buf bytes.Buffer
	renderOverview(&buf, header, rows, at, interactive)
	return buf.String()
}
