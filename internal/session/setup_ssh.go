package session

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// setupSSHAgentForwarding configures an Incus proxy device to forward the host's
// SSH agent socket into the container. This allows git operations inside the
// container to use the host's SSH keys without copying them.
// Returns the container socket path if forwarding was successfully configured,
// or an empty string if forwarding was skipped (no SSH_AUTH_SOCK, invalid socket).
func setupSSHAgentForwarding(mgr *container.Manager, containerName string, logger func(string)) (string, error) {
	hostSocket := filepath.Clean(os.Getenv("SSH_AUTH_SOCK"))
	if hostSocket == "." || hostSocket == "" {
		logger("SSH agent forwarding skipped: SSH_AUTH_SOCK not set on host")
		return "", nil
	}

	// Verify the socket exists and is a Unix domain socket
	if fi, err := os.Stat(hostSocket); err != nil {
		logger(fmt.Sprintf("SSH agent forwarding skipped: socket %s not accessible: %v", hostSocket, err))
		return "", nil
	} else if fi.Mode()&os.ModeSocket == 0 {
		logger(fmt.Sprintf("SSH agent forwarding skipped: %s is not a Unix domain socket", hostSocket))
		return "", nil
	}

	containerSocket := "/tmp/ssh-agent.sock"

	// Remove existing device if present (socket path may have changed between sessions)
	_ = mgr.RemoveDevice("ssh-agent")

	logger(fmt.Sprintf("Forwarding SSH agent: %s -> %s", hostSocket, containerSocket))

	if err := addSSHAgentProxyDevice(mgr, hostSocket, containerSocket); err != nil {
		return "", fmt.Errorf("failed to add SSH agent proxy device: %w", err)
	}

	// Verify the socket appears inside the container. Incus proxy devices can
	// take a moment to create the listen socket, especially on freshly-started
	// containers. If the socket doesn't appear, retry with a remove+re-add
	// which works around an Incus race condition.
	if waitForContainerSocket(mgr, containerSocket, 5*time.Second) {
		return containerSocket, nil
	}

	logger("SSH agent socket not yet available, retrying device setup...")
	_ = mgr.RemoveDevice("ssh-agent")

	if err := addSSHAgentProxyDevice(mgr, hostSocket, containerSocket); err != nil {
		return "", fmt.Errorf("failed to re-add SSH agent proxy device: %w", err)
	}

	if !waitForContainerSocket(mgr, containerSocket, 5*time.Second) {
		logger("Warning: SSH agent socket did not appear in container after retry")
		return "", nil
	}

	return containerSocket, nil
}

// addSSHAgentProxyDevice adds the proxy device for SSH agent forwarding.
func addSSHAgentProxyDevice(mgr *container.Manager, hostSocket, containerSocket string) error {
	return mgr.AddProxyDevice(
		"ssh-agent",
		fmt.Sprintf("unix:%s", hostSocket),
		fmt.Sprintf("unix:%s", containerSocket),
		container.CodeUID,
		container.CodeUID,
	)
}

// waitForContainerSocket polls for a Unix socket to appear inside the container.
func waitForContainerSocket(mgr *container.Manager, socketPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	checkCmd := fmt.Sprintf("test -S %s", socketPath)
	for time.Now().Before(deadline) {
		if _, err := mgr.ExecCommand(checkCmd, container.ExecCommandOptions{Capture: true}); err == nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}
