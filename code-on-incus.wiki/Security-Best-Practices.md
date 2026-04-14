# Security Best Practices

## `cap_linux_immutable` Capability

### What It Does

The `cap_linux_immutable` capability allows the `coi` binary to set and clear the Linux immutable flag (`chattr +i / -i`) on files inside containers. This is used by host-side path protection to prevent containers from tampering with critical mount points.

### Why It's Needed

Without this capability, `coi` cannot enforce immutable protection on paths mounted into containers. The health check (`coi doctor`) will flag this as a warning if the capability is missing.

### Setup

The installer (`install.sh`) grants this capability automatically. For manual installs or after building from source:

```bash
sudo setcap cap_linux_immutable=ep "$(readlink -f "$(which coi)")"
```

> **Note:** `setcap` requires the real binary path, not a symlink. Use `readlink -f` to resolve it.

### After Updates

The capability is stored as a file-level extended attribute and is **lost when the binary is replaced** (the new file gets a new inode). This applies to:

- **`coi update`**: Automatically attempts to restore the capability using `sudo -n setcap`. If it succeeds, a confirmation is printed. If it fails (e.g., no cached `sudo` credentials), it prints the manual command.
- **`make build`** or `go build`: You must re-apply the capability manually:
  ```bash
  sudo setcap cap_linux_immutable=ep "$(readlink -f "$(which coi)")"
  ```

### Verification

Check whether the capability is set:

```bash
getcap "$(readlink -f "$(which coi)")"
```

Expected output:

```
/path/to/coi cap_linux_immutable=ep
```

You can also run `coi doctor` which checks for this capability as part of its health checks.
