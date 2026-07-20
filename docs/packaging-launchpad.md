# Packaging & publishing `coi` on Launchpad

This document describes how `code-on-incus` (the `coi` command) is packaged as a
Debian `.deb` and published to a [Launchpad](https://launchpad.net) PPA, both
manually and via CI.

## How it works

Launchpad does not accept prebuilt binaries. You upload a **signed source
package**; Launchpad's build farm compiles it per-architecture, per-Ubuntu-series,
and publishes an apt repository users can add.

Two facts drive this project's packaging:

1. **`go.mod` requires Go 1.25**, which the Ubuntu LTS archives do not carry
   (jammy ships 1.18, noble 1.22). Go 1.25 comes from
   [`ppa:longsleep/golang-backports`](https://launchpad.net/~longsleep/+archive/ubuntu/golang-backports)
   (package `golang-1.25-go`), added to our PPA as an **archive dependency** so
   the builders can resolve it.
2. **Launchpad builders have no network.** Go modules are therefore *vendored*
   (`go mod vendor`) into the source tarball, and the build runs with
   `GOPROXY=off`. `vendor/` is intentionally not committed (see `.gitignore`);
   it is generated as a build prerequisite locally and in CI.

The build also needs `pkg-config` + `libsystemd-dev` (cgo), and the runtime
needs `libsystemd0` — `coi`'s NFT monitor `dlopen`s `libsystemd.so`, so that
dependency is declared explicitly in `debian/control`.

See `debian/README.source` for the short version.

## One-time setup (manual, only you can do this)

### 1. Launchpad account

Create an account at <https://launchpad.net>. Uploading to a PPA does **not**
require an SSH key or signing the Code of Conduct.

### 2. Create a GPG key

The package `Maintainer` is the upstream author, `Maciej Mensfeld
<maciej@mensfeld.pl>`. The signing key does **not** need to match that —
Launchpad validates the
*signature*, not the maintainer field. What matters is that the key belongs to a
Launchpad account (yours) with upload rights to the PPA. Use your own identity
on the key:

```bash
gpg --full-generate-key
#   Key type: RSA (default), 4096 bits, no expiry (or your preference)
#   Real name: <your-name>
#   Email:     <your-email>

# Note the fingerprint / long key id:
gpg --list-secret-keys --keyid-format=long
```

> **CI note:** the CI workflow signs non-interactively, so use a **passphrase-less**
> key (or a dedicated passphrase-less signing subkey) if you want automated
> uploads. A passphrase-protected key is fine for manual uploads.

### 3. Register the key with Launchpad

```bash
# publish the public key to the keyserver Launchpad reads
gpg --keyserver keyserver.ubuntu.com --send-keys <FINGERPRINT>
```

Then go to <https://launchpad.net/~/+editpgpkeys>, paste the fingerprint, and
submit. Launchpad emails you an **encrypted** confirmation. Decrypt it and open
the link inside:

```bash
# paste the encrypted mail body, Ctrl-D to end
gpg --decrypt
```

### 4. The PPA

Already created: the **`code-on-incus`** team owns
[`ppa:code-on-incus/ppa`](https://launchpad.net/~code-on-incus/+archive/ubuntu/ppa).
Your Launchpad account (the PPA owner) has upload rights, so uploads signed by
your key are accepted — regardless of the package `Maintainer`.

### 5. Add the Go 1.25 archive dependency (critical, not yet done)

Without this, LTS builds fail with "golang-1.25-go has no installation candidate".

1. Open <https://launchpad.net/~code-on-incus/+archive/ubuntu/ppa/+edit-dependencies>.
2. Under "add a new dependency", add **`ppa:longsleep/golang-backports`**.
3. Save.

## Manual upload

From this directory, for a single series (repeat per series with distinct
versions — Launchpad rejects re-uploading the same version to two series):

```bash
go mod vendor                                   # make source offline-buildable

# set the version + target series in the changelog
dch -b -v "0.10.2~ubuntu24.04.1" -D noble "Build 0.10.2 for noble."

debuild --no-lintian -S -sa -d -k<YOUR_KEY_ID>  # build + sign the SOURCE package
                                                # -d: source-only, skip build-dep check
dput ppa:code-on-incus/ppa \
     ../code-on-incus_0.10.2~ubuntu24.04.1_source.changes
```

### Version scheme

Per-series versions are `<upstream>~ubuntu<XX.YY>.<N>` — e.g. `0.10.2~ubuntu24.04.1`.

- **Numeric series, not the codename.** Codenames sort alphabetically and Ubuntu
  restarts the alphabet every cycle (…zesty → artful). Under a `~noble1` style
  scheme the next cycle's series would sort *below* the current one, so `apt`
  would see a downgrade and refuse the upgrade after `do-release-upgrade`.
  `ubuntu24.04` → `ubuntu26.04` → `ubuntu28.04` always sorts forward.
- **No Debian revision.** `3.0 (native)` forbids a hyphen in the version, so the
  usual `0.10.2-1~ubuntu24.04.1` form is not available here.
- **`~` keeps these below a real archive version**, so if `code-on-incus` ever
  lands in Ubuntu proper, the archive package wins over the PPA build.
- **Bump the trailing `.N`** to re-upload the same upstream version to the same
  series (Launchpad rejects a duplicate version).

Watch the build at your PPA's page. Once green, users install with:

```bash
sudo add-apt-repository ppa:code-on-incus/ppa
sudo apt update && sudo apt install code-on-incus   # provides the `coi` command
```

## Automated upload (CI)

`.github/workflows/publish-ppa.yml` builds and uploads a signed source package
for every series. It runs on the **same `v*.*.*` release tags as `release.yml`**,
so cutting a release also publishes to the PPA (plus manual dispatch). The PPA
target is hardcoded to `ppa:code-on-incus/ppa`. The job **no-ops unless
`DEB_EMAIL` is set**, so a fork or upstream without PPA credentials is never
failed by it.

Configure once in **GitHub repo → Settings** (in the repo that owns the release
tags — i.e. upstream once merged):

| Kind     | Name              | Value                                              |
|----------|-------------------|----------------------------------------------------|
| Secret   | `GPG_PRIVATE_KEY` | `gpg --armor --export-secret-keys <FPR>` output    |
| Secret   | `GPG_KEY_ID`      | the key fingerprint / long id used to sign         |
| Variable | `DEB_EMAIL`       | changelog Changed-By email (required; CI toggle)   |
| Variable | `DEB_FULLNAME`    | changelog Changed-By name (optional, default "Code on Incus") |

The signing key just needs upload rights to `ppa:code-on-incus/ppa`; it does not
need to match the changelog `Maintainer` or `DEB_EMAIL` (Launchpad checks the
signature, not those fields).

A release then publishes to the PPA automatically:

```bash
git tag v0.10.2 && git push origin v0.10.2
```

The workflow runs one job per series and uploads each in parallel.

## Reproducing the build locally

Building `.deb`s needs a Debian/Ubuntu-based system. On a non-Debian host
(Fedora, Arch, macOS, ...), run these steps inside an Ubuntu
[distrobox](https://distrobox.it) container (`distrobox enter <name>`) instead.
Either way this mirrors what Launchpad does (backport PPA → Go 1.25 → offline
build):

```bash
sudo add-apt-repository -y ppa:longsleep/golang-backports
sudo apt-get update
sudo apt-get install -y devscripts build-essential fakeroot dpkg-dev \
     debhelper pkg-config libsystemd-dev golang-1.25-go
cd /path/to/launchpad
go mod vendor
dpkg-buildpackage -us -uc -b        # produces ../code-on-incus_*.deb
```

## Notes / limitations

- Source format is `3.0 (native)` for PPA simplicity (one tarball, no
  orig/quilt split). For a real Debian-archive submission, switch to
  `3.0 (quilt)`.
- `debian/copyright` enumerates the licence of every vendored Go module
  (`vendor/`). If `go mod vendor` changes the dependency set, update the
  per-module `Files:` stanzas to match. Verify coverage with
  `licensecheck -r vendor/`.
- The series matrix in the workflow must stay in sync with what
  `longsleep/golang-backports` publishes.
- `coi` shells out to the `incus` CLI and is non-functional without it, so the
  package **`Depends: incus`**. `incus` is in the Ubuntu archive from noble
  (24.04) onward, so it installs automatically there. On **jammy (22.04)**
  `incus` is not in the archive; users must first add the
  [zabbly](https://github.com/zabbly/incus) repo (its package is also named
  `incus`), otherwise `apt install code-on-incus` reports `incus` as
  uninstallable — which correctly signals the missing prerequisite.
- Other host tools are handled without extra hard dependencies:
  - `nftables`, `iproute2`, `iptables`, `uidmap`, `rsync`, and `systemd`
    (journalctl/systemctl) are pulled in transitively by `incus` (via
    `incus-base`), so `coi`'s firewall/monitoring/cleanup paths get them for
    free — no need to re-declare them.
  - `sudo`, `git`, and `openssh-client` (used on the host for privileged
    operations, git identity, and SSH-credential mounting) are `Recommends`:
    installed by default, but `coi` still partly functions without each.
  - Container-side tools (`tmux`, the agent tooling) run *inside* the Incus
    container via `incus exec`, so they are provided by the container image,
    not the host package.
- Integration tests are skipped during the package build (they need a live
  Incus daemon).
- **The self-updater is disabled in packaged builds.** `debian/rules` stamps
  `-X internal/cli.InstallSource=deb` next to the version ldflag, and
  `coi update core` refuses when that is set to anything but `source`.
  Otherwise it would overwrite the dpkg-owned `/usr/bin/coi` in place —
  desyncing dpkg's file database, with the next `apt upgrade` silently
  reverting the update. `--force` does not override this; `--check` still
  works, as does `coi update patterns` (the detection databases are not
  shipped in the package). Any future packaging target (rpm, Arch, ...) gets
  the same protection by stamping its own `InstallSource` value; adding it to
  `packageUpdateCommands` in `internal/cli/update.go` upgrades the message
  from generic advice to that package manager's exact command.
- Lintian runs as a separate **non-fatal** CI step rather than via `debuild`,
  so packaging tags are visible in the log without a lintian regression
  blocking a release. Read it when you touch `debian/`.
