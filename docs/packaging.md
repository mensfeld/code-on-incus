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

The changelog `Maintainer` is `William Black <maciej@mensfeld.pl>`, so the
key's user id **must** use that same email.

```bash
gpg --full-generate-key
#   Key type: RSA (default), 4096 bits, no expiry (or your preference)
#   Real name: William Black
#   Email:     maciej@mensfeld.pl

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
Your Launchpad account (William Black, PPA owner) has upload rights, and its GPG
key must match the `Maintainer` email above.

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
dch -b -v "0.10.2~noble1" -D noble "Build 0.10.2 for noble."

debuild -S -sa -k<YOUR_KEY_ID>                  # build + sign the SOURCE package
dput ppa:code-on-incus/ppa \
     ../code-on-incus_0.10.2~noble1_source.changes
```

Watch the build at your PPA's page. Once green, users install with:

```bash
sudo add-apt-repository ppa:code-on-incus/ppa
sudo apt update && sudo apt install code-on-incus   # provides the `coi` command
```

## Automated upload (CI)

`.github/workflows/publish-ppa.yml` builds and uploads a signed source package
for every series. It runs on the **same `v*.*.*` release tags as `release.yml`**,
so cutting a release also publishes to the PPA (plus manual dispatch). The job
**no-ops unless `LAUNCHPAD_PPA` is set**, so a fork or upstream without PPA
credentials is never failed by it.

Configure once in **GitHub repo → Settings** (in the repo that owns the release
tags — i.e. upstream once merged):

| Kind     | Name              | Value                                              |
|----------|-------------------|----------------------------------------------------|
| Secret   | `GPG_PRIVATE_KEY` | `gpg --armor --export-secret-keys <FPR>` output    |
| Secret   | `GPG_KEY_ID`      | the key fingerprint / long id used to sign         |
| Variable | `LAUNCHPAD_PPA`   | `ppa:code-on-incus/ppa`                            |

The signing key must have upload rights to `ppa:code-on-incus/ppa` and its
user-id email must match `debian/changelog`'s `Maintainer`.

A release then publishes to the PPA automatically:

```bash
git tag v0.10.2 && git push origin v0.10.2
```

The workflow runs one job per series and uploads each in parallel.

## Reproducing the build locally (distrobox)

The Fedora host can't build `.deb`s, but a distrobox Ubuntu container can. This
mirrors exactly what Launchpad does (backport PPA → Go 1.25 → offline build):

```bash
distrobox enter code-on-incus-ubuntu
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
  `3.0 (quilt)` and enumerate third-party licenses in `debian/copyright`.
- The series matrix in the workflow must stay in sync with what
  `longsleep/golang-backports` publishes.
- Integration tests are skipped during the package build (they need a live
  Incus daemon).
