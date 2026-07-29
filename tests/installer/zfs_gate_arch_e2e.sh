#!/usr/bin/env bash
#
# End-to-end regression test for #666 on a REAL non-apt distro (Arch Linux).
#
# The stubbed Go tests in internal/installer/install_sh_test.go source install.sh
# with a hand-set PKG_MANAGER and mocked binaries. This test is the real thing:
# it runs the ACTUAL install.sh storage-selection logic inside a genuine Arch
# container, where `pacman` is really on PATH (so detect_pkg_manager resolves
# "pacman" on its own) and `command -v zfs` is genuinely false. It proves the
# installer never hands a ZFS package to the package manager on Arch — the
# install that rebuilds and breaks the initramfs (#666) — and selects btrfs
# instead, which is exactly the reporter's environment.
#
# The privileged package-install and incus calls are intercepted by a recording
# `sudo` shim: we must NOT actually run `pacman -S zfs*` (that IS the destructive
# action the fix prevents), and there is no incus daemon in the container. So
# this asserts the DECISION install.sh makes, driven by real distro tooling —
# not a real storage-pool creation.
#
# Modes (via env):
#   default                    btrfs tools absent  -> installer must reach for
#                              the SAFE btrfs-progs package, never a zfs package.
#   COI_E2E_BTRFS_PRESENT=1    btrfs tools present -> the reporter's exact case:
#                              no package install happens at all, btrfs is used.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INSTALL_SH="$REPO_ROOT/install.sh"

BTRFS_PRESENT="${COI_E2E_BTRFS_PRESENT:-0}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

sudo_log="$workdir/sudo.log"
: > "$sudo_log"

# Recording shim for sudo: log every invocation and neutralise the two commands
# setup_fast_storage would otherwise run for real (a package install and an incus
# pool creation). Logging everything lets the assertions inspect what was tried.
cat > "$workdir/sudo" <<SHIM
#!/usr/bin/env bash
echo "\$*" >> "$sudo_log"
exit 0
SHIM
chmod +x "$workdir/sudo"

# No-op incus: report no existing pools, succeed on everything else.
cat > "$workdir/incus" <<'SHIM'
#!/usr/bin/env bash
if [[ "$*" == *"storage list"* ]]; then echo ""; exit 0; fi
exit 0
SHIM
chmod +x "$workdir/incus"

# Reporter's case: btrfs tooling already present. A fake mkfs.btrfs makes
# setup_btrfs_storage take its "already installed" path, so no package install
# is attempted at all.
if [ "$BTRFS_PRESENT" = "1" ]; then
    printf '#!/usr/bin/env bash\nexit 0\n' > "$workdir/mkfs.btrfs"
    chmod +x "$workdir/mkfs.btrfs"
fi

# Prepend the shims but keep the real system PATH, so real `pacman`, real
# `command -v zfs`, and real `uname` all still resolve normally.
export PATH="$workdir:$PATH"
export NONINTERACTIVE=1

# Premise checks: this must be a real non-apt (pacman) box with ZFS genuinely
# absent, otherwise the test proves nothing and must fail loudly rather than
# silently pass. Every environmental assumption the assertions rely on is
# asserted here so a drifting base image fails clearly, not misleadingly.
command -v pacman >/dev/null 2>&1 || { echo "FAIL: pacman not found; not an Arch host"; exit 1; }
if command -v apt-get >/dev/null 2>&1; then
    echo "FAIL: apt-get present; not a pure non-apt host"; exit 1
fi
# ZFS must be absent in BOTH modes — the whole point is that setup_fast_storage
# takes its non-ZFS branch; if zfs were present it would run ZFS setup and the
# assertions below would fail with a confusing message instead of this clear one.
if command -v zfs >/dev/null 2>&1; then
    echo "FAIL: zfs is installed on the runner; test premise broken"; exit 1
fi
# Default mode asserts the installer *reaches for* btrfs-progs, which only happens
# when btrfs tooling is absent. If a future base image ships it, say so plainly
# rather than failing later with 'did not reach for btrfs-progs'.
if [ "$BTRFS_PRESENT" != "1" ] && command -v mkfs.btrfs >/dev/null 2>&1; then
    echo "FAIL: btrfs tools already present; default mode needs them absent (use COI_E2E_BTRFS_PRESENT=1)"; exit 1
fi

# Source the REAL installer (minus its entrypoint and ERR trap) and drive the
# real storage selection through the real package-manager detection. The `\$`
# escapes sed's regex metacharacter so the pattern matches the literal `main "$@"`.
# shellcheck disable=SC1090
source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "$INSTALL_SH")
set +e  # setup_* return non-zero on soft failures; we assert on output, not $?.

detect_pkg_manager
if [ "${PKG_MANAGER:-}" != "pacman" ]; then
    echo "FAIL: detect_pkg_manager resolved '${PKG_MANAGER:-<unset>}', expected pacman"
    exit 1
fi

out="$(setup_fast_storage 2>&1)"
echo "===== mode: BTRFS_PRESENT=$BTRFS_PRESENT ====="
echo "----- setup_fast_storage output -----"
echo "$out"
echo "----- recorded privileged (sudo) calls -----"
cat "$sudo_log"
echo "---------------------------------------------"

fail=0

# 1. The regression guard: no ZFS-related privileged command was ever issued.
if grep -Eiq 'zfs' "$sudo_log"; then
    echo "FAIL(#666): installer issued a ZFS-related privileged command on Arch:"
    grep -Ei 'zfs' "$sudo_log"
    fail=1
fi

# 2. The ZFS setup path must never have been entered.
if grep -qF "Setting up fast storage (ZFS)" <<<"$out"; then
    echo "FAIL(#666): ZFS storage setup ran on a non-apt distro"
    fail=1
fi

# 3. btrfs must have been chosen instead.
if ! grep -qF "Setting up fast storage (btrfs)" <<<"$out"; then
    echo "FAIL: btrfs storage was not selected"
    fail=1
fi

# 4. Mode-specific: the package the installer reached for (if any).
if [ "$BTRFS_PRESENT" = "1" ]; then
    # btrfs already present -> no package install at all.
    if grep -Eq 'pacman -S' "$sudo_log"; then
        echo "FAIL: a package install was attempted even though btrfs tools were present:"
        grep -E 'pacman -S' "$sudo_log"
        fail=1
    fi
else
    # btrfs absent -> the ONLY package it may install is the safe btrfs-progs.
    if ! grep -q 'btrfs-progs' "$sudo_log"; then
        echo "FAIL: installer did not reach for btrfs-progs when btrfs was absent"
        fail=1
    fi
fi

if [ "$fail" -ne 0 ]; then
    echo "RESULT: FAILED"
    exit 1
fi
echo "RESULT: PASSED — install.sh skipped ZFS and selected btrfs on a real Arch host (#666)"
