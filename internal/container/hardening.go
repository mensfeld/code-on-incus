package container

import (
	"fmt"
	"strconv"
	"strings"
)

// HardeningPolicy selects how much kernel-facing surface a container gets.
// The zero value is maximally hardened; DefaultHardeningPolicy() preserves the
// historical behavior (Docker support on).
type HardeningPolicy struct {
	// Docker configures the container for Docker/nested containers:
	// security.nesting, mknod/setxattr syscall interception, and the
	// unprivileged low-port sysctl. Off, those four knobs are unset,
	// shrinking the shared-kernel attack surface.
	Docker bool
	// ReduceKernelSurface additionally denies the syscall families behind
	// most recent kernel escape chains via security.syscalls.deny. It wins
	// over Docker: nesting is part of the surface being reduced, and dockerd
	// itself needs bpf.
	ReduceKernelSurface bool
	// ReduceKernelSurfaceStrict is the opt-in strict tier: everything
	// ReduceKernelSurface does, plus perf_event_open on the deny list.
	// perf_event_open has a long kernel-LPE history and nothing in a normal
	// coding workflow needs it, but denying it removes kernel-level profiling
	// (perf, JVM async-profiler's perf mode) — a real capability loss, unlike
	// the base list's transparently-substituted syscalls — so it is a separate
	// opt-in rather than part of the default hardened list. Implies (and does
	// not require separately setting) ReduceKernelSurface.
	ReduceKernelSurfaceStrict bool
}

// reducesKernelSurface reports whether either hardening tier is active. The
// strict tier implies the base tier, so callers never have to set both.
func (p HardeningPolicy) reducesKernelSurface() bool {
	return p.ReduceKernelSurface || p.ReduceKernelSurfaceStrict
}

// DefaultHardeningPolicy returns the pre-flag behavior: Docker support on,
// no syscall denies.
func DefaultHardeningPolicy() HardeningPolicy { return HardeningPolicy{Docker: true} }

// DockerEnabled resolves the docker/hardening conflict: either kernel-surface
// tier wins over Docker (nesting is part of the surface being reduced).
func (p HardeningPolicy) DockerEnabled() bool { return p.Docker && !p.reducesKernelSurface() }

// KernelSurfaceDenySyscalls is the security.syscalls.deny value applied by
// ReduceKernelSurface: io_uring, bpf, userfaultfd, and the kernel keyring —
// the syscall families behind most recent container-escape/LPE chains, none
// required by a typical agent workflow. Denied syscalls fail with EPERM;
// libuv (Node >= 20.3) probes io_uring at startup and silently falls back to
// its thread pool, so Node/npm keep working. Incus >= 6.1 (COI's hard floor)
// always supports the key.
const KernelSurfaceDenySyscalls = "io_uring_setup io_uring_enter io_uring_register bpf userfaultfd keyctl add_key request_key"

// KernelSurfaceStrictExtraSyscalls are the additional denies of the opt-in
// strict tier (ReduceKernelSurfaceStrict) on top of KernelSurfaceDenySyscalls.
// perf_event_open is a long-standing kernel-LPE vector unused by normal coding
// workflows; unlike the base list it removes real capability (kernel profiling
// via perf / async-profiler degrades gracefully — EPERM, no crash — but stops
// working), which is why it lives behind a separate opt-in.
const KernelSurfaceStrictExtraSyscalls = "perf_event_open"

// kernelSurfaceDenyList is the security.syscalls.deny value for a policy: empty
// when no tier is active, the base list under ReduceKernelSurface, or the base
// list plus the strict extras under ReduceKernelSurfaceStrict.
func kernelSurfaceDenyList(p HardeningPolicy) string {
	if !p.reducesKernelSurface() {
		return ""
	}
	if p.ReduceKernelSurfaceStrict {
		return KernelSurfaceDenySyscalls + " " + KernelSurfaceStrictExtraSyscalls
	}
	return KernelSurfaceDenySyscalls
}

// kernelSurfaceKeys are the instance config keys ApplyKernelSurfacePolicy owns.
// Every launch/reconcile writes ALL of them (a wanted key to its value, an
// unwanted key to "") so re-applying a changed policy to an existing stopped
// container converges instead of leaking the previous policy's settings. An
// empty value removes the key (Incus treats `config set key=` as unset), so
// the whole policy applies in ONE `incus config set` — atomic and fail-closed.
var kernelSurfaceKeys = []string{
	"security.nesting",
	"security.syscalls.intercept.mknod",
	"security.syscalls.intercept.setxattr",
	"linux.sysctl.net.ipv4.ip_unprivileged_port_start",
	"security.syscalls.deny",
}

// hardeningConfigArgs returns the `key=value` pairs realizing the policy: the
// four Docker-support keys set to their values (or "" to unset) per
// DockerEnabled, and security.syscalls.deny set to the deny list (or "") per
// ReduceKernelSurface. Every key in kernelSurfaceKeys appears exactly once so
// the result fully specifies the policy.
func hardeningConfigArgs(p HardeningPolicy) []string {
	docker := "" // "" unsets the key
	if p.DockerEnabled() {
		docker = "true"
	}
	lowPort := ""
	if p.DockerEnabled() {
		lowPort = "0"
	}
	deny := kernelSurfaceDenyList(p)
	return []string{
		"security.nesting=" + docker,
		"security.syscalls.intercept.mknod=" + docker,
		"security.syscalls.intercept.setxattr=" + docker,
		"linux.sysctl.net.ipv4.ip_unprivileged_port_start=" + lowPort,
		"security.syscalls.deny=" + deny,
	}
}

// ApplyKernelSurfacePolicy applies (or re-applies) the hardening policy to a
// container in a single `incus config set`. Like the Docker flags it replaces,
// this must run before the container's first boot so the kernel loads the
// correct seccomp profile — setting security.nesting or security.syscalls.deny
// on a running container is rejected or racy. It is idempotent: call it again
// on a STOPPED persistent container to converge it to a changed policy. Unlike
// the previous per-key implementation it is FAIL-CLOSED — a failed write
// returns an error (the caller must decide, e.g. abort the launch) rather than
// silently leaving a hardened config partly applied.
func ApplyKernelSurfacePolicy(containerName string, p HardeningPolicy) error {
	args := append([]string{"config", "set", containerName}, hardeningConfigArgs(p)...)
	return IncusExec(args...)
}

// ReadKernelSurfaceConfig returns the INSTANCE-LOCAL values of the keys
// ApplyKernelSurfacePolicy owns — the layer COI actually writes, so the
// convergence check compares like with like and reaches a stable state in one
// write even when an attached Incus profile also supplies these keys (profile
// values are invisible here on purpose; whether they DEFEAT the policy is the
// separate, expanded-config question VerifyKernelSurfacePolicy answers). A key
// absent from the config maps to "" in the result.
func ReadKernelSurfaceConfig(containerName string) (map[string]string, error) {
	return readKernelSurfaceConfig(containerName, false)
}

func readKernelSurfaceConfig(containerName string, expanded bool) (map[string]string, error) {
	result := make(map[string]string, len(kernelSurfaceKeys))
	for _, key := range kernelSurfaceKeys {
		args := []string{"config", "get"}
		if expanded {
			args = append(args, "--expanded")
		}
		args = append(args, containerName, key)
		v, err := IncusOutput(args...)
		if err != nil {
			return nil, fmt.Errorf("read %s from %s: %w", key, containerName, err)
		}
		result[key] = v
	}
	return result, nil
}

// ReconcileKernelSurfacePolicy converges an existing STOPPED container to the
// policy, but only when its instance-local config does not already match — so
// a reuse that changes nothing performs no writes and, crucially, cannot
// clobber a deny list or nesting value the container already carries. Returns
// whether a change was made. Fail-closed twice over: a read or write failure
// is returned so the caller can abort rather than start a container whose
// kernel-surface config is unknown or half-applied, and after converging it
// verifies the EFFECTIVE (profile-inherited) surface actually honors the
// policy — an instance-local unset cannot override a profile-supplied
// security.nesting=true, and booting anyway would silently defeat the
// hardening the user asked for.
func ReconcileKernelSurfacePolicy(containerName string, p HardeningPolicy) (changed bool, err error) {
	current, err := ReadKernelSurfaceConfig(containerName)
	if err != nil {
		return false, err
	}
	if !KernelSurfaceMatches(current, p) {
		if err := ApplyKernelSurfacePolicy(containerName, p); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, VerifyKernelSurfacePolicy(containerName, p)
}

// VerifyKernelSurfacePolicy checks the EXPANDED (profile-inherited) config
// against the policy and errors when the effective kernel surface is WIDER
// than the policy allows — the one case instance-local writes cannot fix,
// because a profile-supplied value survives an instance-local unset. Free for
// the default policy (docker on wants every surface key set, and instance-local
// values always override profiles, so there is nothing a profile could widen —
// no reads are performed). Values the profile only NARROWS (e.g. an extra
// syscall deny under the default policy) are tolerated: they are the user's
// own tightening, not a policy violation.
func VerifyKernelSurfacePolicy(containerName string, p HardeningPolicy) error {
	if p.DockerEnabled() {
		return nil
	}
	expanded, err := readKernelSurfaceConfig(containerName, true)
	if err != nil {
		return err
	}
	if v := kernelSurfaceViolations(expanded, p); len(v) > 0 {
		return fmt.Errorf(
			"kernel-surface policy cannot be enforced: %s remain(s) in effect via an attached Incus profile, and an instance-local unset cannot override profile config; remove the key(s) from the container's Incus profile(s), or relax [container] docker / [security] reduce_kernel_surface",
			strings.Join(v, ", "))
	}
	return nil
}

// kernelSurfaceViolations is the pure core of VerifyKernelSurfacePolicy: given
// EXPANDED config, it lists the keys whose effective values widen the surface
// beyond the policy. Boolean keys use Incus's truthy spellings (true/1/yes/on);
// the low-port sysctl only widens below the kernel default of 1024 (an
// unparseable value fails closed as a violation).
func kernelSurfaceViolations(expanded map[string]string, p HardeningPolicy) []string {
	if p.DockerEnabled() {
		return nil
	}
	var violations []string
	for _, key := range []string{
		"security.nesting",
		"security.syscalls.intercept.mknod",
		"security.syscalls.intercept.setxattr",
	} {
		if incusTruthy(expanded[key]) {
			violations = append(violations, key+"="+strings.TrimSpace(expanded[key]))
		}
	}
	if s := strings.TrimSpace(expanded["linux.sysctl.net.ipv4.ip_unprivileged_port_start"]); s != "" {
		if n, err := strconv.Atoi(s); err != nil || n < 1024 {
			violations = append(violations, "linux.sysctl.net.ipv4.ip_unprivileged_port_start="+s)
		}
	}
	if p.reducesKernelSurface() {
		// Instance-local deny (which we write) overrides any profile deny, so
		// this cannot realistically fire — kept as a cheap invariant so a
		// future write-path regression surfaces here instead of shipping a
		// hardened container without its deny list.
		have := make(map[string]bool)
		for _, tok := range strings.Fields(expanded["security.syscalls.deny"]) {
			have[tok] = true
		}
		var missing []string
		for _, tok := range strings.Fields(kernelSurfaceDenyList(p)) {
			if !have[tok] {
				missing = append(missing, tok)
			}
		}
		if len(missing) > 0 {
			violations = append(violations, "security.syscalls.deny missing "+strings.Join(missing, " "))
		}
	}
	return violations
}

// incusTruthy reports whether an Incus boolean config value is effectively
// true, accepting the spellings Incus itself accepts (true/1/yes/on,
// case-insensitive).
func incusTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// KernelSurfaceMatches reports whether current (as returned by
// ReadKernelSurfaceConfig, instance-local) already realizes the policy, so a
// reconcile can skip the write — avoiding both wasted `config set` calls and
// clobbering a container whose config already matches. A missing key reads
// as "".
func KernelSurfaceMatches(current map[string]string, p HardeningPolicy) bool {
	for _, kv := range hardeningConfigArgs(p) {
		key, want, _ := strings.Cut(kv, "=")
		if strings.TrimSpace(current[key]) != want {
			return false
		}
	}
	return true
}
