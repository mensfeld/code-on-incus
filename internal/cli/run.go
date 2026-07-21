package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/mensfeld/code-on-incus/internal/alias"
	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/spf13/cobra"
)

// runScriptName is the workspace run-script convention: when `coi run` is
// invoked with no command, an executable file with this name at the workspace
// root is executed inside the container (straight from the workspace mount).
// It is extensionless on purpose: the shebang decides the interpreter, so a
// bash, ruby, or python script all work the same way.
const runScriptName = "coi-run"

var runCmd = &cobra.Command{
	Use:   "run [command] [args...]",
	Short: "Run a command or the workspace run script in a sandboxed container",
	Long: `Execute a command in an isolated Incus container with the full sandbox
(workspace mount, protected paths, secret masking, network isolation, limits,
monitoring). Output streams live and the command's exit code is propagated.

With no command, coi looks for an executable ` + runScriptName + ` at the workspace
root and runs it inside the container directly from the workspace mount. The
shebang decides the interpreter, so any language works (#!/usr/bin/env bash,
ruby, python, ...).

The container is ephemeral: it is cleaned up after the command completes (or
stopped and kept, when [container] persistent = true is configured).

Examples:
  coi run                            # run ./` + runScriptName + ` in the sandbox
  coi run --profile scripts          # same, with a credential-limiting profile
  coi run -- npm test                # run an arbitrary command
  coi run --workspace ~/project -- make build
`,
	Args: cobra.ArbitraryArgs,
	RunE: app.runCommand,
}

// detectRunScript checks for the workspace run script. The script must carry
// the executable bit — it is executed directly (the shebang decides the
// interpreter), so a non-executable file is an error with a chmod hint rather
// than a silent guess at how to interpret it.
//
// A symlink is followed only if it resolves INSIDE the workspace: the script
// executes from the workspace mount, so a target outside it would pass the
// host-side check but not exist in the container — the exact opaque failure
// this fail-fast detection exists to prevent (same rule as secret_paths
// symlink handling).
func detectRunScript(absWorkspace string) (found bool, err error) {
	scriptPath := filepath.Join(absWorkspace, runScriptName)
	if _, lerr := os.Lstat(scriptPath); lerr != nil {
		if os.IsNotExist(lerr) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check for %s: %w", runScriptName, lerr)
	}

	resolved, rerr := filepath.EvalSymlinks(scriptPath)
	if rerr != nil {
		return false, fmt.Errorf("%s exists but cannot be resolved (dangling symlink?): %w", runScriptName, rerr)
	}
	resolvedWorkspace, rerr := filepath.EvalSymlinks(absWorkspace)
	if rerr != nil {
		return false, fmt.Errorf("failed to resolve workspace path: %w", rerr)
	}
	if resolved != resolvedWorkspace && !strings.HasPrefix(resolved, resolvedWorkspace+string(filepath.Separator)) {
		return false, fmt.Errorf("%s is a symlink resolving outside the workspace (%s) — the target will not exist inside the container; move the script into the workspace or replace the symlink with a copy", runScriptName, resolved)
	}

	fi, statErr := os.Stat(scriptPath)
	if statErr != nil {
		return false, fmt.Errorf("failed to check for %s: %w", runScriptName, statErr)
	}
	if fi.IsDir() {
		return false, fmt.Errorf("%s in workspace is a directory, expected a script file", runScriptName)
	}
	if fi.Mode()&0o111 == 0 {
		return false, fmt.Errorf("%s exists but is not executable — its shebang decides the interpreter, so make it executable: chmod +x %s", runScriptName, runScriptName)
	}
	return true, nil
}

func (a *App) runCommand(cmd *cobra.Command, args []string) error {
	// Signal-aware context so phases can abort on Ctrl+C.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Resolve workspace and validate max_duration before starting the pipeline
	// so errors surface before any container work.
	absWorkspace, err := filepath.Abs(a.workspace)
	if err != nil {
		return fmt.Errorf("invalid workspace path: %w", err)
	}
	if err := a.overlayWorkspaceConfig(absWorkspace); err != nil {
		return err
	}
	// No --profile given: fall back to [defaults] profile (#607). coi run has no
	// alias/resume profile source, so this is the only fallback site it needs.
	if applied, err := a.applyDefaultProfileFallback(cmd); err != nil {
		return err
	} else if applied {
		a.persistent = config.BoolVal(a.cfg.Container.Persistent)
	}
	if a.cfg.Limits.Runtime.MaxDuration != "" {
		if _, err := limits.ParseDuration(a.cfg.Limits.Runtime.MaxDuration); err != nil {
			return fmt.Errorf("invalid max_duration: %w", err)
		}
	}

	s := &runState{absWorkspace: absWorkspace}

	// No command given: fall back to the workspace run-script convention.
	// Detection happens host-side before any container work so a missing
	// script fails fast with a clear message.
	if len(args) == 0 {
		found, err := detectRunScript(absWorkspace)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("no command given and no %s found in workspace %s\n"+
				"Create an executable %s at the workspace root or pass a command: coi run -- <command>",
				runScriptName, absWorkspace, runScriptName)
		}
		s.runScript = true
	}

	pipeline := &session.Pipeline{}
	defer pipeline.Teardown()

	// Signal handler: trigger cleanup immediately on SIGINT while incus exec blocks.
	// Uses a dedicated sigChan (not ctx.Done) to avoid non-determinism when both fire
	// simultaneously. pipeline.Teardown is idempotent so the deferred call above is safe.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sigChan:
			fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, cleaning up...\n")
			pipeline.Teardown()
		case <-done:
		}
	}()

	return pipeline.Run(
		ctx,
		a.validateEnvRunPhase(s),
		a.launchContainerRunPhase(s),
		a.configureContainerRunPhase(s),
		a.applyNetworkRunPhase(s),
		a.startMonitoringRunPhase(s),
		a.runCommandPhase(args, s),
	)
}

// launchOrReuseContainer restarts an existing persistent container, or
// recreates / creates a fresh one on the given storage pool.
func launchOrReuseContainer(mgr container.ContainerManager, img, pool, containerName string, containerExists, persistent bool, preStart, preRestart func() error) error {
	if containerExists && persistent {
		// Fail fast on an already-running container (another session may own
		// it). Master's plain Start() errored here; StartWithIsolationFallback's
		// came-up-anyway poll would misread "already running" as success and let
		// two sessions share one container — the first to exit would stop it
		// under the second.
		if running, _ := mgr.Running(); running {
			return fmt.Errorf("container %s is already running — another session may be using it; pick a different --slot or stop it first", containerName)
		}
		// Reconcile start-time-only state on the STOPPED container before starting:
		// the workspace-sourced security devices are stripped and re-established
		// against the current workspace, so a source removed while stopped can't
		// wedge the start with "Missing source path" (issue #610). Mirrors preStart
		// on the fresh path; must run before StartWithIsolationFallback.
		if preRestart != nil {
			if err := preRestart(); err != nil {
				return fmt.Errorf("failed to reconcile container before restart: %w", err)
			}
		}
		fmt.Fprintf(os.Stderr, "Restarting existing persistent container...\n")
		// Start with the isolation fallback: the container's disk devices (set at
		// creation) materialize at start, and one on an idmap-incompatible
		// filesystem (virtiofs workspace, #534) fails the start under
		// security.idmap.isolated — same class the fresh-launch path handles.
		// Safe for persistent containers: a failed start does not delete them.
		if err := container.StartWithIsolationFallback(containerName); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		return nil
	}
	if containerExists {
		fmt.Fprintf(os.Stderr, "Removing existing container...\n")
		if err := mgr.Delete(true); err != nil {
			return fmt.Errorf("failed to delete existing container: %w", err)
		}
	}
	ephemeral := !persistent
	// preStart applies start-time-only settings between init and first start:
	// raw.idmap for the workspace UID mapping (#530) and every disk device, so
	// idmap-incompatible device filesystems fail the START where the isolation
	// fallback covers them (#534).
	if err := mgr.LaunchWithPreStart(img, ephemeral, pool, preStart); err != nil {
		// Best-effort cleanup of the half-created container: a preStart/start
		// failure leaves it behind stopped and half-configured (stopped
		// ephemeral containers are only auto-deleted after having run, and a
		// persistent corpse would be reused broken by the next run). Never
		// delete a RUNNING container — if a concurrent invocation won the name
		// race (our init failed with "already exists"), it is theirs.
		if running, _ := mgr.Running(); !running {
			_ = mgr.Delete(true)
		}
		return fmt.Errorf("failed to launch container: %w", err)
	}
	return nil
}

// ensureBridgeTrustedZone ensures the Incus bridge has iptables FORWARD ACCEPT
// rules before launching a container. Without this, DHCP replies are dropped and
// the container never gets an IP when the FORWARD policy is DROP.
func ensureBridgeTrustedZone() {
	if changed, bridgeName, err := network.EnsureBridgeInTrustedZone(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not ensure bridge forwarding rules: %v\n", err)
	} else if changed {
		fmt.Fprintf(os.Stderr, "Added iptables FORWARD rules for %s (was missing — containers could not get IPs)\n", bridgeName)
	}
}

// remapContainerUserIfNeeded remaps the container's 'code' user UID/GID from the
// image default (1000) to the configured container.CodeUID. Only runs for fresh
// containers that actually have the code user (probed at runtime, since
// custom images built FROM coi-default also inherit it) when the
// configured UID differs from the image default.
func remapContainerUserIfNeeded(mgr container.ContainerManager, wasRestarted bool) error {
	if wasRestarted || container.CodeUID == 1000 {
		return nil
	}
	hasCodeUser, err := session.DetectCodeUser(mgr, container.CodeUser)
	if err != nil {
		// Surface connectivity / exec failures to stderr so an unexpectedly
		// skipped remap doesn't look like a silent success. We still proceed
		// without remapping — the alternative would be to abort `coi run`
		// on a transient probe failure, which is worse UX.
		fmt.Fprintf(os.Stderr,
			"Warning: could not probe container for %s user, skipping UID/GID remap: %v\n",
			container.CodeUser, err)
		return nil
	}
	if !hasCodeUser {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Remapping user %s from UID 1000 to %d...\n", container.CodeUser, container.CodeUID)
	remapCmd := fmt.Sprintf(
		"groupmod -g %d %s && usermod -u %d -g %d %s",
		container.CodeUID, container.CodeUser,
		container.CodeUID, container.CodeUID, container.CodeUser,
	)
	if _, err := mgr.ExecCommand(remapCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		// usermod -u, even without -m, walks the home directory chowning
		// files it finds owned by the old UID — that walk hits any
		// read-only mount already living under /home/<code> (e.g. a
		// protected-path or user [[mounts]] entry, disk devices attach
		// pre-start per #534) and usermod exits non-zero despite the
		// actual UID/GID change having applied. Don't trust the exit
		// code alone: probe whether the account really did move to the
		// target UID before treating this as fatal.
		actualUID, probeErr := session.ResolveCodeUID(mgr, container.CodeUser)
		if probeErr != nil || actualUID != container.CodeUID {
			return fmt.Errorf("failed to remap user %s to UID %d: %w", container.CodeUser, container.CodeUID, err)
		}
		fmt.Fprintf(os.Stderr,
			"Warning: UID/GID remap for %s succeeded but the home-directory ownership walk hit a read-only mount: %v\n",
			container.CodeUser, err)
	}
	// The home-ownership sweep is best-effort, separately from the remap
	// itself: disk devices attach pre-start now (#534), so a user-configured
	// mount under /home/<code> is already live here — a readonly one makes
	// chown -R exit non-zero after fixing everything it could, which must not
	// abort the run (the remap succeeded; only some mounted paths kept their
	// ownership, as they would have on the host).
	chownCmd := fmt.Sprintf("chown -R %s:%s /home/%s", container.CodeUser, container.CodeUser, container.CodeUser)
	if _, err := mgr.ExecCommand(chownCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		fmt.Fprintf(os.Stderr,
			"Warning: could not chown all of /home/%s after UID remap (a read-only mount under it is expected to fail): %v\n",
			container.CodeUser, err)
	}
	return nil
}

// appendEnvArgs appends --env flags for config environment and forward_env
// to an incus exec args slice.
// tz is the resolved timezone name (may be empty).
// socketEnv maps env var names to container-side socket paths for every
// forwarded socket (SSH_AUTH_SOCK plus any configured [[sockets]] entries).
func (a *App) appendEnvArgs(incusArgs []string, tz string, socketEnv map[string]string) ([]string, error) {
	// Assemble the environment in a map so precedence is deterministic: each
	// source in turn overwrites the previous one (last-wins), and every key is
	// emitted as a single --env flag below. This mirrors how `coi shell` builds
	// its env (buildContainerEnv) and, crucially, does NOT depend on how incus
	// resolves repeated --env flags — no key is ever passed twice.
	//
	// Order = increasing priority:
	//   1. baseline identity (HOME/USER/LOGNAME)
	//   2. timezone
	//   3. forwarded sockets
	//   4. [defaults].environment (+ profile environment)
	//   5. forward_env (host values)
	//   6. env_commands (freshly minted per session)
	env := map[string]string{}

	// Baseline identity. incus exec does not set HOME/USER for a --user exec, so
	// without this a `coi run` command runs with no HOME — anything resolving ~
	// or reading a --global config breaks (`git config --global` ->
	// "fatal: $HOME not set", #623). `coi run` execs as the code user, whose home
	// is /home/<code_user>. A user can still override any of these via
	// [defaults].environment (they are applied later, below).
	env["HOME"] = "/home/" + container.CodeUser
	env["USER"] = container.CodeUser
	env["LOGNAME"] = container.CodeUser

	if tz != "" {
		env["TZ"] = tz
	}

	for name, path := range socketEnv {
		env[name] = path
	}

	// Static environment from config (defaults.environment + profile environment)
	for k, v := range a.cfg.Defaults.Environment {
		env[k] = v
	}

	// Resolve forward_env from config, look up host values
	for _, name := range a.cfg.Defaults.ForwardEnv {
		if val, ok := os.LookupEnv(name); ok {
			env[name] = val
		} else {
			fmt.Fprintf(os.Stderr, "Warning: forward_env variable %q is not set on host, skipping\n", name)
		}
	}

	// Command-sourced env vars (highest precedence — freshly minted per session).
	// A failing env_command is fatal: don't launch a half-credentialed session.
	envCommandValues, err := a.resolveEnvCommands()
	if err != nil {
		return nil, err
	}
	for k, v := range envCommandValues {
		env[k] = v
	}

	// Emit each var once, in a stable (sorted) order for deterministic args.
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		incusArgs = append(incusArgs, "--env", fmt.Sprintf("%s=%s", k, env[k]))
	}

	return incusArgs, nil
}

// hasAnyLimits checks if any limits are configured (used in run.go)
func hasAnyLimits(cfg *config.LimitsConfig) bool {
	if cfg == nil {
		return false
	}

	// Check if any limit is set (non-empty strings or non-zero integers)
	return cfg.CPU.Count != "" ||
		cfg.CPU.Allowance != "" ||
		cfg.CPU.Priority != 0 ||
		cfg.Memory.Limit != "" ||
		cfg.Memory.Enforce != "" ||
		cfg.Memory.Swap != "" ||
		cfg.Disk.Read != "" ||
		cfg.Disk.Write != "" ||
		cfg.Disk.Max != "" ||
		cfg.Disk.Priority != 0 ||
		cfg.Runtime.MaxProcesses != 0
}

// filterWritableGitHooks removes .git/hooks from protected paths when writable hooks are enabled.
func filterWritableGitHooks(paths []string, cfg *config.Config) []string {
	if !config.BoolVal(cfg.Git.WritableHooks) {
		return paths
	}
	gitHooksSuffix := filepath.Join(".git", "hooks")
	filtered := paths[:0]
	for _, p := range paths {
		if !strings.HasSuffix(p, gitHooksSuffix) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// validateAndPrepareAlias validates the alias from config and returns it.
// Returns ("", nil) if no alias is configured.
func validateAndPrepareAlias(aliasStr string) (string, error) {
	if aliasStr == "" {
		return "", nil
	}
	if err := alias.ValidateAlias(aliasStr); err != nil {
		return "", fmt.Errorf("invalid alias: %w", err)
	}
	return aliasStr, nil
}

// applyContainerAlias sets the user.coi.alias metadata on the container and
// registers the alias in the global registry. No-op if alias is empty.
func applyContainerAlias(effectiveAlias, containerName, absWorkspace string) error {
	if effectiveAlias == "" {
		return nil
	}

	if err := container.IncusExec("config", "set", containerName,
		fmt.Sprintf("user.coi.alias=%s", effectiveAlias)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to set alias metadata: %v\n", err)
	}

	reg, err := alias.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load alias registry: %v\n", err)
		return nil
	}
	if err := reg.Register(effectiveAlias, absWorkspace, ""); err != nil {
		return fmt.Errorf("alias conflict: %w", err)
	}
	if err := reg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save alias registry: %v\n", err)
	}

	return nil
}

// overlayWorkspaceConfig loads the workspace-local .coi/config.toml on top of
// the global config when --workspace points to a directory other than the CWD.
// config.Load() already loads <CWD>/.coi/config.toml, so skipping the overlay
// when they match avoids doubling entries like AdditionalProtectedPaths.
func (a *App) overlayWorkspaceConfig(absWorkspace string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("failed to resolve working directory: %w", err)
	}
	if absWorkspace == absCWD {
		return nil
	}
	if err := a.cfg.OverlayProjectConfig(absWorkspace); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load project config from %s: %w", absWorkspace, err)
	}
	container.Configure(a.cfg.Incus.Project, a.cfg.Incus.CodeUser, a.cfg.Incus.CodeUID)
	// An explicitly requested --profile must keep winning over the workspace
	// overlay for [container] settings — otherwise a project config could
	// silently override the profile the user asked for (and precedence would
	// depend on whether the command ran from inside the workspace or not).
	if a.profile != "" {
		if err := a.cfg.ReapplyProfileContainer(a.profile); err != nil {
			return err
		}
	}
	// The workspace config may set [container] persistent — PersistentPreRunE
	// computed a.persistent before this overlay ran, so recompute it here.
	a.persistent = config.BoolVal(a.cfg.Container.Persistent)
	return nil
}

// resolveContainerWorkspacePath returns the path inside the container where the
// workspace should be mounted. When preserve_workspace_path is set it mirrors
// the host path, falling back to /workspace if the path conflicts with system dirs.
func (a *App) resolveContainerWorkspacePath(absWorkspace string, forcePreserve bool) string {
	if !a.cfg.Paths.PreserveWorkspacePath && !forcePreserve {
		return "/workspace"
	}
	if session.WorkspaceUnderSystemDir(absWorkspace) {
		// A worktree caller pre-checks this and hard-errors (it can't work at
		// /workspace); here it's the plain preserve_workspace_path fallback.
		fmt.Fprintf(os.Stderr, "Warning: preserve_workspace_path requested for %q conflicts with system directories; using /workspace instead\n", absWorkspace)
		return "/workspace"
	}
	return filepath.Clean(absWorkspace)
}

// applyWorkspaceMounts mounts the workspace and all configured additional directories
// into the container, then applies security (read-only) mounts. For restarted persistent
// containers it retrieves the existing workspace path from the container config instead.
func (a *App) applyWorkspaceMounts(mgr container.ContainerManager, containerName, absWorkspace string, containerWorkspacePath *string, mountConfig *session.MountConfig, useShift, wasRestarted bool, worktree *session.GitWorktreeLayout) error {
	if wasRestarted {
		fmt.Fprintf(os.Stderr, "Reusing existing workspace mount...\n")
		*containerWorkspacePath = mgr.GetWorkspacePath()
		return nil
	}

	if *containerWorkspacePath == absWorkspace {
		fmt.Fprintf(os.Stderr, "Mounting workspace %s -> %s (preserving host path)...\n", absWorkspace, *containerWorkspacePath)
	} else {
		fmt.Fprintf(os.Stderr, "Mounting workspace %s -> %s...\n", absWorkspace, *containerWorkspacePath)
	}
	if err := mgr.MountDisk("workspace", absWorkspace, *containerWorkspacePath, useShift, false); err != nil {
		return fmt.Errorf("failed to mount workspace: %w", err)
	}

	// Mount the worktree's external git common dir read-write at its host path so
	// git resolves; its RCE sinks are re-covered read-only in applySecurityMounts
	// (issue #533). Must precede the security mounts so the overlays layer on top.
	if worktree != nil {
		if err := session.MountGitWorktreeDirs(mgr, worktree, useShift); err != nil {
			return fmt.Errorf("failed to mount git worktree dirs: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Mounted git worktree common dir (read-write): %s\n", worktree.CommonDir)
	}

	if err := session.ValidateMounts(mountConfig); err != nil {
		return fmt.Errorf("mount validation failed: %w", err)
	}
	if mountConfig != nil {
		for _, mount := range mountConfig.Mounts {
			if err := addMount(mgr, mount, useShift); err != nil {
				return err
			}
		}
	}

	// Called unconditionally: applySecurityMounts internally skips the read-only
	// protected_paths when disable_protection is set (GetEffectiveProtectedPaths
	// returns nil), but secret-path masking is a separate opt-in that still runs.
	if err := a.applySecurityMounts(mgr, absWorkspace, *containerWorkspacePath, containerName, useShift, worktree); err != nil {
		return err
	}
	return nil
}

// addMount adds a single configured directory mount to the container.
func addMount(mgr container.ContainerManager, mount session.MountEntry, useShift bool) error {
	if mount.Readonly {
		if _, err := os.Stat(mount.HostPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Warning: readonly mount source %s does not exist, skipping\n", mount.HostPath)
				return nil
			}
			return fmt.Errorf("failed to stat readonly mount source '%s': %w", mount.HostPath, err)
		}
		fmt.Fprintf(os.Stderr, "Adding mount (read-only): %s -> %s\n", mount.HostPath, mount.ContainerPath)
	} else {
		if err := os.MkdirAll(mount.HostPath, 0o755); err != nil {
			return fmt.Errorf("failed to create mount directory '%s': %w", mount.HostPath, err)
		}
		fmt.Fprintf(os.Stderr, "Adding mount: %s -> %s\n", mount.HostPath, mount.ContainerPath)
	}
	if err := mgr.MountDisk(mount.DeviceName, mount.HostPath, mount.ContainerPath, useShift, mount.Readonly); err != nil {
		return fmt.Errorf("failed to add mount '%s': %w", mount.DeviceName, err)
	}
	return nil
}

// applySecurityMounts sets up read-only protection mounts and optional host immutable flags.
func (a *App) applySecurityMounts(mgr container.ContainerManager, absWorkspace, containerWorkspacePath, containerName string, useShift bool, worktree *session.GitWorktreeLayout) error {
	protectedPaths := filterWritableGitHooks(a.cfg.Security.GetEffectiveProtectedPaths(), a.cfg)
	if worktree != nil {
		// The workspace .git is a file; its .git/* defaults can't be protected here
		// (they'd each warn "gitdir indirection") — SetupCommonDirProtection covers
		// the real internals below. Keep the non-.git protections (.vscode, ...).
		protectedPaths = session.StripGitProtectedPaths(protectedPaths)
	}
	// SetupSecurityMounts expands the dynamic per-worktree git configs internally
	// (issue #545 — the single chokepoint both `coi run` and `coi shell` share) and
	// returns the effective list to drive logging + the immutable pass over the same
	// set. The immutable pass runs even on a partial mount error, so it is gated on
	// the returned list rather than the error.
	effectivePaths, err := session.SetupSecurityMounts(mgr, absWorkspace, containerWorkspacePath, protectedPaths, useShift, &a.cfg.Security)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to setup security mounts: %v\n", err)
	} else if len(effectivePaths) > 0 {
		actualPaths := session.GetProtectedPathsForLogging(absWorkspace, effectivePaths)
		if len(actualPaths) > 0 {
			fmt.Fprintf(os.Stderr, "Protected paths (mounted read-only): %s\n", strings.Join(actualPaths, ", "))
		}
	}
	if len(effectivePaths) > 0 && a.cfg.Security.IsHostImmutableEnabled() {
		logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
		immutablePaths := session.ApplyImmutable(absWorkspace, effectivePaths, containerName, logFn)
		if len(immutablePaths) > 0 {
			fmt.Fprintf(os.Stderr, "Host-side immutable protection applied: %s\n", strings.Join(immutablePaths, ", "))
		}
	}

	// Extend read-only protection to the external git common dir for a worktree
	// checkout (#533): the same #474 hook/config lockdown applied to the real
	// internals the workspace-relative pass above cannot reach. Mount-only (the
	// host-immutable belt is deferred; the read-only mount is the primary control).
	if worktree != nil {
		writableHooks := config.BoolVal(a.cfg.Git.WritableHooks)
		cProtected, cErr := session.SetupCommonDirProtection(mgr, worktree.CommonDir, useShift, writableHooks, &a.cfg.Security)
		if cErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: some git common-dir protections not applied: %v\n", cErr)
		}
		if len(cProtected) > 0 {
			fmt.Fprintf(os.Stderr, "Protected git common-dir paths (read-only): %s\n", strings.Join(cProtected, ", "))
		}
	}

	// Mask secret paths (issue #494) — independent of protected_paths.
	if len(a.cfg.Security.SecretPaths) > 0 {
		masked, skipped, err := session.SetupSecretMasks(mgr, absWorkspace, containerWorkspacePath, a.cfg.Security.SecretPaths, useShift)
		for _, s := range skipped {
			fmt.Fprintf(os.Stderr, "Warning: secret_paths entry %q is NOT masked (missing or a symlink resolving outside the workspace) — not hidden from the agent\n", s)
		}
		if err != nil {
			return fmt.Errorf("failed to mask secret paths: %w", err)
		}
		if len(masked) > 0 {
			fmt.Fprintf(os.Stderr, "Masked secret paths (hidden read-only): %s\n", strings.Join(masked, ", "))
		}
	}
	return nil
}

// gateRunForwarding drops untrusted, unapproved mounts and sockets from project
// config (combined trust fingerprint over both). Sockets are re-forwarded on
// every launch, so the gate always applies to them; a reused persistent
// container keeps its creation-time mount devices, so for those we only warn
// that trust changes won't take effect until the container is recreated.
func (a *App) gateRunForwarding(mc *session.MountConfig, sc *session.SocketConfig, pc *session.PortConfig, workspace string, wasRestarted bool) (*session.MountConfig, *session.SocketConfig, *session.PortConfig) {
	keptMC, droppedM, keptSC, droppedS, _, _, keptPC, droppedP := session.FilterTrusted(mc, sc, nil, pc, workspace)
	if wasRestarted {
		if len(droppedM) > 0 {
			fmt.Fprintf(os.Stderr,
				"Warning: %d untrusted mount(s) remain attached from when this persistent "+
					"container was created; recreate it (coi kill + relaunch) to apply mount-trust changes.\n",
				len(droppedM))
		}
	} else {
		warnDroppedMounts(droppedM)
	}
	warnDroppedSockets(droppedS)
	warnDroppedPorts(droppedP)
	return keptMC, keptSC, keptPC
}

// applyForwardSockets forwards the host SSH agent (when ssh.forward_agent is
// true) plus every trust-gated [[sockets]] entry into the container. Returns a
// map of env var name -> container-side socket path for those that declare one.
func (a *App) applyForwardSockets(mgr container.ContainerManager, socketConfig *session.SocketConfig) map[string]string {
	logger := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
	return session.ForwardConfiguredSockets(mgr, socketConfig, config.BoolVal(a.cfg.SSH.ForwardAgent), logger)
}

// applyNetworkIsolation installs firewall rules for the container when
// network.mode is set to something other than "open" in config.
// Returns the Manager so the caller can Teardown before container deletion.
func (a *App) applyNetworkIsolation(ctx context.Context, containerName string) (*network.Manager, error) {
	networkConfig := a.cfg.Network
	if networkConfig.Mode == "" || networkConfig.Mode == config.NetworkModeOpen {
		return nil, nil
	}
	if changed, bridgeName, err := network.EnsureBridgeInTrustedZone(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not ensure bridge forwarding rules: %v\n", err)
	} else if changed {
		fmt.Fprintf(os.Stderr, "Added iptables FORWARD rules for %s\n", bridgeName)
	}
	nm := network.NewManager(&networkConfig, logger.NewDiscard())
	if err := nm.SetupForContainer(ctx, containerName); err != nil {
		return nil, fmt.Errorf("failed to setup network isolation: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Network isolation applied: %s\n", networkConfig.Mode)
	return nm, nil
}

// applyContainerTimezone resolves the timezone and configures it inside the container.
// Returns the resolved timezone name (empty for UTC).
func (a *App) applyContainerTimezone(mgr container.ContainerManager) string {
	tz := resolveTimezone(a.cfg)
	if tz != "" {
		tzCmd := fmt.Sprintf(
			"ln -sf /usr/share/zoneinfo/%s /etc/localtime && echo %s > /etc/timezone",
			tz, tz,
		)
		if _, err := mgr.ExecCommand(tzCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to set timezone: %v\n", err)
		}
	} else {
		// Explicitly reset to UTC — important for persistent containers that may
		// have had a different timezone applied in a previous session.
		resetCmd := "ln -sf /usr/share/zoneinfo/UTC /etc/localtime && echo UTC > /etc/timezone"
		if _, err := mgr.ExecCommand(resetCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to reset timezone to UTC: %v\n", err)
		}
	}
	return tz
}
