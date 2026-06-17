package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// defaultEnvCommandTimeout bounds each env_commands invocation when the config
// does not set [defaults] env_command_timeout.
const defaultEnvCommandTimeout = 30 * time.Second

// resolveEnvCommands runs each configured [defaults.env_commands] command on the
// host and returns a map of env var name -> trimmed stdout, to be injected into
// the container. By the time this runs, untrusted-source entries have already
// been stripped at config load (internal/config/loader.go), so every command
// here comes from trusted-scope config.
//
// A failing or timing-out command aborts the launch (returns an error) rather
// than starting a session without its minted credential. Commands run via
// `sh -c` so `~` expansion and pipelines work. Keys are processed in sorted
// order for deterministic behavior.
func (a *App) resolveEnvCommands() (map[string]string, error) {
	commands := a.cfg.Defaults.EnvCommands
	if len(commands) == 0 {
		return nil, nil
	}

	timeout := a.envCommandTimeout()

	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	resolved := make(map[string]string, len(commands))
	for _, name := range names {
		cmd := commands[name]
		value, err := runEnvCommand(cmd, timeout)
		if err != nil {
			return nil, fmt.Errorf("env_command for %q (%q) failed: %w", name, cmd, err)
		}
		resolved[name] = value
	}
	return resolved, nil
}

// envCommandTimeout resolves the configured per-command timeout, falling back to
// the default on an empty or invalid value (warning on invalid).
func (a *App) envCommandTimeout() time.Duration {
	raw := strings.TrimSpace(a.cfg.Defaults.EnvCommandTimeout)
	if raw == "" {
		return defaultEnvCommandTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr,
			"Warning: invalid env_command_timeout %q, using default %s\n", raw, defaultEnvCommandTimeout)
		return defaultEnvCommandTimeout
	}
	return d
}

// runEnvCommand executes a single host command with the given timeout and
// returns its trimmed stdout. On non-zero exit it surfaces stderr in the error.
func runEnvCommand(command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stderr strings.Builder
	cmd.Stderr = &stderr

	stdout, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(stdout)), nil
}
