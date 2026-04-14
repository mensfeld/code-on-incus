# Self-Update

`coi update` downloads and installs the latest release from GitHub.

## Usage

```bash
coi update          # Download and install latest release
coi update --check  # Only check for updates, don't install
coi update --force  # Skip confirmation prompt
```

## How It Works

1. Queries the GitHub releases API for the latest version
2. Compares with the currently installed version
3. Downloads the platform-specific binary
4. Verifies the SHA256 checksum
5. Atomically replaces the current binary (write to temp file, then rename)
6. Restores `cap_linux_immutable` capability (Linux only)

## Notes

- If the binary directory is not writable, `coi update` suggests re-running with `sudo`.
- Development builds (`dev` version) require `--force` to update, since they cannot be version-compared.
- On Linux, `coi update` automatically attempts to restore the `cap_linux_immutable` capability after replacing the binary. The capability is stored as a file-level extended attribute and is lost when the binary is replaced (new inode). If the automatic restore fails (e.g., no cached `sudo` credentials), the command prints the manual command:
  ```bash
  sudo setcap cap_linux_immutable=ep /path/to/coi
  ```
