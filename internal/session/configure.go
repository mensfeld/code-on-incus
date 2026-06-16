package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/network"
)

// ConfigureOptions contains options for configuring a running container.
// This applies post-boot settings (network, SSH, timezone, security, etc.)
// without managing container lifecycle. Designed for programmatic use by
// orchestrators like coi-pond that manage their own container lifecycle.
type ConfigureOptions struct {
	ContainerName string // Required: name of a running container
	WorkspacePath string // Host workspace path (for security protection context)

	// Features to apply (all optional)
	NetworkConfig *config.NetworkConfig // Network isolation mode and rules
	LimitsConfig  *config.LimitsConfig  // Runtime limits (max_duration, max_processes)
	ForwardSSH    bool                  // Forward host SSH agent into container
	Timezone      string                // IANA timezone name (e.g., "Europe/Warsaw"), empty = UTC
	ToolName      string                // "claude" triggers managed settings injection
	GitGuard      bool                  // Set git user.useConfigOnly=true

	Logger func(string)
}

// ConfigureResult contains the results of configuring a container.
type ConfigureResult struct {
	SSHAgentSocketPath string `json:"ssh_agent_socket_path,omitempty"`
	Timezone           string `json:"timezone,omitempty"`
	NetworkApplied     string `json:"network_applied,omitempty"`
	HomeDir            string `json:"home_dir"`
}

// ConfigureContainer applies post-boot settings to a running container.
// It reuses the same logic as session.Setup() but operates on an already-running
// container, allowing orchestrators to manage container lifecycle independently
// while still leveraging coi's network isolation, SSH forwarding, timezone,
// git identity guard, and managed settings features.
func ConfigureContainer(ctx context.Context, opts ConfigureOptions) (*ConfigureResult, error) {
	if opts.ContainerName == "" {
		return nil, fmt.Errorf("container name is required")
	}

	if opts.Logger == nil {
		opts.Logger = func(msg string) {
			fmt.Fprintf(os.Stderr, "[configure] %s\n", msg)
		}
	}

	mgr := container.NewManager(opts.ContainerName)

	// Verify container is running
	running, err := mgr.Running()
	if err != nil {
		return nil, fmt.Errorf("failed to check container status: %w", err)
	}
	if !running {
		return nil, fmt.Errorf("container %s is not running", opts.ContainerName)
	}

	// Detect code user to determine home directory
	hasCodeUser, err := DetectCodeUser(mgr, container.CodeUser)
	if err != nil {
		opts.Logger(fmt.Sprintf("Warning: could not probe for %s user: %v — assuming root", container.CodeUser, err))
		hasCodeUser = false
	}

	homeDir := "/root"
	if hasCodeUser {
		homeDir = "/home/" + container.CodeUser
	}

	result := &ConfigureResult{
		HomeDir: homeDir,
	}

	// 1. SSH agent forwarding
	if opts.ForwardSSH {
		socketPath, err := SetupSSHAgentForwarding(mgr, opts.ContainerName, opts.Logger)
		if err != nil {
			opts.Logger(fmt.Sprintf("Warning: SSH agent forwarding failed: %v", err))
		} else if socketPath != "" {
			result.SSHAgentSocketPath = socketPath
		}
	}

	// 2. Git identity guard
	if opts.GitGuard {
		SetupGitIdentityGuard(mgr, homeDir, opts.Logger)
	}

	// 3. Claude managed settings
	if opts.ToolName == "claude" {
		SetupClaudeManagedSettings(mgr, opts.Logger)
	}

	// 4. Timezone
	result.Timezone = opts.Timezone
	if opts.Timezone != "" {
		opts.Logger(fmt.Sprintf("Setting timezone to %s...", opts.Timezone))
		tzCmd := fmt.Sprintf(
			"ln -sf /usr/share/zoneinfo/%s /etc/localtime && echo %s > /etc/timezone",
			opts.Timezone, opts.Timezone,
		)
		if _, err := mgr.ExecCommand(tzCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to set timezone: %v", err))
		}
	}

	// 5. Mise trust for workspace
	if opts.WorkspacePath != "" {
		containerWorkspacePath := mgr.GetWorkspacePath()
		if containerWorkspacePath == "" {
			containerWorkspacePath = "/workspace"
		}
		SetupMiseTrust(mgr, containerWorkspacePath, opts.Logger)
	}

	// 6. Network isolation
	if opts.NetworkConfig != nil && opts.NetworkConfig.Mode != "" && opts.NetworkConfig.Mode != config.NetworkModeOpen {
		// Ensure bridge is in trusted zone before applying network rules
		if changed, bridgeName, err := network.EnsureBridgeInTrustedZone(); err != nil {
			opts.Logger(fmt.Sprintf("Warning: could not ensure bridge forwarding rules: %v", err))
		} else if changed {
			opts.Logger(fmt.Sprintf("Added iptables FORWARD rules for %s", bridgeName))
		}

		nm := network.NewManager(opts.NetworkConfig, logger.NewDiscard())
		if err := nm.SetupForContainer(ctx, opts.ContainerName); err != nil {
			return nil, fmt.Errorf("failed to setup network isolation: %w", err)
		}
		result.NetworkApplied = string(opts.NetworkConfig.Mode)
		opts.Logger(fmt.Sprintf("Network isolation applied: %s", opts.NetworkConfig.Mode))
	}

	// 7. Runtime limits (timeout monitor, max_processes)
	// Note: CPU/memory/disk limits must be applied before container start via
	// coi container launch flags. Only runtime limits can be applied post-boot.
	if opts.LimitsConfig != nil {
		if opts.LimitsConfig.Runtime.MaxProcesses != 0 {
			applyOpts := limits.ApplyOptions{
				ContainerName: opts.ContainerName,
				Runtime: limits.RuntimeLimits{
					MaxProcesses: opts.LimitsConfig.Runtime.MaxProcesses,
				},
			}
			if err := limits.ApplyResourceLimits(applyOpts); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to apply runtime limits: %v", err))
			}
		}
		// Note: MaxDuration timeout monitor is not started here because it requires
		// a long-running process. The caller should manage session timeouts.
	}

	opts.Logger("Container configuration complete!")
	return result, nil
}

// ConfigureResultJSON returns the result as a JSON string for CLI output.
func ConfigureResultJSON(result *ConfigureResult) (string, error) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	return string(data), nil
}
