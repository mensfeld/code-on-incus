# Docker in Incus Container Investigation

## Container Information
- **Container Name:** `coi-044a148a-1`
- **Kernel Version:** 6.14.0-37-generic
- **Virtualization Type:** LXC/Incus
- **Date:** 2026-01-26

## Problem Description

Docker daemon runs successfully inside the Incus container, but Docker containers fail to start with the following error:

```
docker: Error response from daemon: failed to create task for container: failed to create shim task:
OCI runtime create failed: runc create failed: unable to start container process: error during
container init: open sysctl net.ipv4.ip_unprivileged_port_start file: reopen fd 8: permission denied
```

## Investigation Findings

### What Works
- ✅ Docker daemon is running (`systemd status docker` shows active)
- ✅ Docker containers with `--network host` work perfectly
- ✅ Manual network namespace creation works (`unshare --net`)
- ✅ The sysctl file `/proc/sys/net/ipv4/ip_unprivileged_port_start` is readable
- ✅ User namespaces are enabled (`kernel.unprivileged_userns_clone = 1`)
- ✅ AppArmor profile is `unconfined`

### What Doesn't Work
- ❌ Creating containers with isolated network namespaces (default behavior)
- ❌ The failure occurs during `runc create` when initializing the container
- ❌ Happens specifically when runc tries to reopen file descriptor 8 pointing to the sysctl file

### Root Cause

When Docker (runc) creates a new network namespace for containers, it needs to access and reopen file descriptors for network-related sysctls. The Incus container doesn't have the proper nested containerization flags enabled, which prevents runc from performing these operations inside a network namespace.

The issue is NOT with:
- Docker configuration
- Kernel capabilities (checked with `/proc/self/status`)
- Seccomp filters (tested with `--security-opt seccomp=unconfined`)
- File permissions on the sysctl

The issue IS with:
- Missing Incus container configuration for nested containerization
- Network namespace setup restrictions in runc

## Solution

Run these commands **from the Incus host** (not inside the container):

```bash
# Enable container nesting support
incus config set coi-044a148a-1 security.nesting=true

# Enable syscall interception for mknod (required for device node creation)
incus config set coi-044a148a-1 security.syscalls.intercept.mknod=true

# Enable syscall interception for setxattr (required for filesystem attributes)
incus config set coi-044a148a-1 security.syscalls.intercept.setxattr=true

# Restart the container to apply changes
incus restart coi-044a148a-1
```

### Testing After Fix

Once the container restarts, test with:

```bash
# Test basic container
sudo docker run --rm alpine:latest echo "Docker is working!"

# Test Apache HTTP server
sudo docker run -d -p 8080:80 --name test-apache httpd:latest

# Verify it's running
sudo docker ps

# Test the web server
curl http://localhost:8080

# Cleanup
sudo docker stop test-apache
sudo docker rm test-apache
```

## Security Considerations - Are These Flags Safe?

### `security.nesting=true`

**What it does:** Allows containers inside the Incus container to use kernel features needed for containerization (cgroups, namespaces, etc.)

**Security Impact:**
- ⚠️ **Medium risk** - Increases attack surface by allowing nested containers
- Containers inside can create their own isolated environments
- If a process escapes the Docker container, it's still within the Incus container
- Does NOT give access to the host system

**Safe if:**
- The Incus container is used for development/testing
- You trust the code running inside Docker containers
- The Incus container itself is properly isolated from production systems

### `security.syscalls.intercept.mknod=true`

**What it does:** Allows Incus to intercept and safely handle `mknod` syscalls (device node creation) instead of blocking them

**Security Impact:**
- ✅ **Low risk** - Incus intercepts these calls and handles them safely
- Prevents nested containers from creating arbitrary device nodes on the host
- Required for Docker to create pseudo-devices like `/dev/null`, `/dev/zero` inside containers

**Safe if:** Using Incus 5.0+ (which properly implements syscall interception)

### `security.syscalls.intercept.setxattr=true`

**What it does:** Allows Incus to intercept and safely handle `setxattr` syscalls (setting extended file attributes)

**Security Impact:**
- ✅ **Low risk** - Incus intercepts and controls what attributes can be set
- Needed for Docker's overlay filesystem operations
- Prevents nested containers from setting security-sensitive attributes directly

**Safe if:** Using Incus 5.0+ with syscall interception support

## Overall Safety Assessment

**✅ SAFE for development/testing environments** where:
- The container is used for development work
- You control what runs inside Docker containers
- The Incus host is not a production server with sensitive data

**⚠️ USE CAUTION in production** where:
- The container runs untrusted workloads
- Multiple users have access to the container
- The Incus host contains sensitive data or services

**🔒 Best Practices:**
- Only enable nesting when specifically needed for Docker/Podman/LXD workloads
- Keep the Incus host system updated
- Monitor container resource usage
- Use Incus resource limits (CPU, memory, disk) to prevent DoS
- Consider using Incus projects for additional isolation

## Alternative Solutions (If You Don't Want to Enable Nesting)

1. **Use host networking for all containers:**
   ```bash
   docker run --network host ...
   ```
   - Less isolation but avoids the network namespace issue

2. **Run Docker directly on the host instead of in Incus**
   - Best isolation but less flexible

3. **Use Incus VMs instead of containers**
   - Full virtualization provides complete isolation
   - Slightly higher resource overhead

## References

- Incus nesting documentation: https://linuxcontainers.org/incus/docs/main/reference/devices_nic/
- Docker in LXD/Incus: https://wiki.ubuntu.com/LXD/Docker
- Syscall interception: https://linuxcontainers.org/incus/docs/main/syscall-interception/
