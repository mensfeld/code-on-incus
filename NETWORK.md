# Network Isolation in COI

## Overview

COI implements network isolation to prevent containers from accessing local/internal networks while allowing full internet access for development workflows. This protects against lateral movement and reconnaissance attacks while maintaining compatibility with package registries, APIs, and other internet services.

## Security Model

### Threat Model

**Primary Threat**: Container accessing internal/host network for reconnaissance or exfiltration
- Scanning host network for open ports
- Accessing private services (databases, admin panels, internal APIs)
- Exfiltrating data via cloud metadata endpoints
- Lateral movement to other machines on the network

**Out of Scope** (for v1):
- DNS tunneling (DNS is required for development)
- Data exfiltration to public internet (allowed by design)
- Host compromise (containers are unprivileged)

### Defense Strategy

**Simplified Approach**: Block local/internal networks only
- All internet traffic allowed (no domain allowlisting)
- Private networks (RFC1918) blocked
- Cloud metadata endpoints blocked
- Host can access container services (ingress allowed)

This covers the primary security concern (lateral movement) without impacting development workflows.

## Network Modes

### Restricted Mode (Default)

**What it does:**
- Blocks egress to RFC1918 private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
- Blocks egress to cloud metadata endpoints (169.254.0.0/16)
- Allows ALL public internet traffic
- Allows ingress from host (so you can access web servers in containers)

**Use case**: Default for all sessions, provides security without impacting development

```bash
coi shell                        # Default: restricted mode
coi shell --network=restricted   # Explicit
```

### Open Mode

**What it does:**
- No network restrictions at all
- Full access to local networks and internet
- Same behavior as pre-isolation COI

**Use case**: Fully trusted projects or when you need local network access

```bash
coi shell --network=open
```

## Configuration

### Config File

```toml
# ~/.config/coi/config.toml

[network]
mode = "restricted"  # restricted | open (default: restricted)
block_private_networks = true
block_metadata_endpoint = true

[network.logging]
enabled = true
path = "~/.coi/logs/network.log"
```

### CLI Flags

```bash
coi shell --network=restricted   # Use restricted mode
coi shell --network=open         # Use open mode
```

CLI flags override config file settings.

### Profile-Based Configuration

```toml
# ~/.config/coi/config.toml

[profiles.secure]
network.mode = "restricted"

[profiles.trusted]
network.mode = "open"

[profiles.development]
network.mode = "restricted"
network.block_metadata_endpoint = false  # Allow metadata for testing
```

Usage:
```bash
coi shell --profile secure    # Uses restricted mode
coi shell --profile trusted   # Uses open mode
```

## Implementation

### Architecture

**Components:**

1. **NetworkConfig** (`internal/config/config.go`)
   - Configuration structure
   - Network mode enum (restricted, open)
   - Boolean flags for specific blocks

2. **ACLManager** (`internal/network/acl.go`)
   - Creates Incus network ACLs
   - Builds reject rules for blocked networks
   - Applies ACLs to container NICs
   - Deletes ACLs on cleanup

3. **NetworkManager** (`internal/network/manager.go`)
   - High-level interface
   - Orchestrates ACL creation and application
   - Handles different network modes
   - Logs network setup

4. **Session Integration** (`internal/session/setup.go`, `cleanup.go`)
   - Calls NetworkManager during container setup
   - Cleans up ACLs during container teardown
   - Passes config from CLI to network layer

### Incus Network ACLs

**How it works:**

Incus ACLs are applied to container network interfaces (NICs) and filter egress traffic at the network level:

1. Create ACL with allow and rejection rules:
   ```bash
   incus network acl create coi-container-restricted
   # First, add allow rule for all traffic (default allow)
   incus network acl rule add coi-container-restricted egress action=allow
   # Then add specific reject rules (evaluated first due to specificity)
   incus network acl rule add coi-container-restricted egress action=reject destination=10.0.0.0/8
   incus network acl rule add coi-container-restricted egress action=reject destination=172.16.0.0/12
   incus network acl rule add coi-container-restricted egress action=reject destination=192.168.0.0/16
   incus network acl rule add coi-container-restricted egress action=reject destination=169.254.0.0/16
   ```

2. Apply ACL to container:
   ```bash
   incus config device set <container> eth0 security.acls=coi-container-restricted
   ```

3. ACL rules are enforced by Incus (no container escape possible)

**Default behavior:**
- When no ACL is applied: all traffic allowed
- When an ACL is applied with only reject rules: all traffic blocked (default deny)
- Solution: Add an explicit "allow all" rule to permit non-rejected traffic
- Our implementation adds `egress action=allow` as the first rule, followed by specific reject rules

**Ingress:**
- No ingress rules needed
- Host can freely access container services
- Egress ACLs don't affect ingress

### ACL Rule Generation

Rules are built based on configuration:

```go
func buildACLRules(cfg *config.NetworkConfig) []string {
    rules := []string{}

    if cfg.Mode == config.NetworkModeRestricted {
        // First, add allow rule for all traffic (required to avoid default deny)
        rules = append(rules, "egress action=allow")

        // Then add specific reject rules
        if cfg.BlockPrivateNetworks {
            rules = append(rules, "egress action=reject destination=10.0.0.0/8")
            rules = append(rules, "egress action=reject destination=172.16.0.0/12")
            rules = append(rules, "egress action=reject destination=192.168.0.0/16")
        }

        if cfg.BlockMetadataEndpoint {
            rules = append(rules, "egress action=reject destination=169.254.0.0/16")
        }
    }

    return rules
}
```

## Testing Network Isolation

### Manual Testing

```bash
# Test restricted mode (default) - blocks local networks
coi shell
> curl example.com          # ✓ Should work (public internet)
> curl registry.npmjs.org   # ✓ Should work (public internet)
> curl 192.168.1.1          # ✗ Should FAIL (private network blocked)
> curl 169.254.169.254      # ✗ Should FAIL (metadata endpoint blocked)
> curl 10.0.0.1             # ✗ Should FAIL (private network blocked)

# Test open mode - allows everything
coi shell --network=open
> curl 192.168.1.1          # ✓ Should work (no restrictions)
> curl example.com          # ✓ Should work

# Test host access to container service
coi shell
> (in container) python3 -m http.server 8000
# (from host) curl http://<container-ip>:8000
# ✓ Should work - ingress allowed
```

### Automated Testing

```bash
# Run network isolation tests
pytest tests/network/ -v

# Run all tests (including network tests)
pytest tests/ -v
```

## Security Limitations

### Known Limitations

1. **DNS Tunneling**
   - DNS is allowed (required for development)
   - Data can be exfiltrated via DNS queries
   - Mitigations: Use controlled DNS server (future enhancement)

2. **Data Exfiltration to Internet**
   - All public internet traffic is allowed
   - No domain allowlisting or blocking
   - This is by design to avoid impacting development workflows

3. **Application-Level Proxies**
   - Container could run HTTP proxy to bypass restrictions
   - Requires explicit action by code in container

4. **Not a Security Boundary**
   - This is defense-in-depth, not absolute protection
   - Containers are still unprivileged and isolated
   - Primary goal: prevent accidental/opportunistic network scanning

### Recommended Practices

1. **Use Restricted Mode by Default**
   - Only use open mode for trusted projects
   - Be aware of what code you're asking AI to execute

2. **Monitor Container Activity**
   - Check logs for suspicious network activity
   - Use `coi container logs <name>` to inspect container behavior

3. **Limit Exposed Credentials**
   - Don't mount sensitive credentials into containers
   - Use temporary/scoped credentials when possible

4. **Network Segmentation**
   - Run COI on a separate network segment
   - Use firewall rules for additional protection

## Future Enhancements

### Planned Features (Out of Scope for v1)

1. **Connection Logging**
   - Log all connection attempts with eBPF
   - Track which domains are accessed
   - Detect suspicious patterns

2. **DNS Filtering**
   - Use controlled DNS server to block tunneling
   - Allowlist specific domains

3. **Egress Proxy**
   - Route all traffic through filtering proxy
   - Deep packet inspection for exfiltration detection

4. **iptables Fallback**
   - For non-standard network configurations
   - When Incus ACLs aren't available

5. **Learning Mode**
   - Auto-discover domains during session
   - Build per-project allowlists
   - Suggest restrictions based on observed behavior

## Troubleshooting

### Container Can't Reach Internet

**Symptom**: `curl example.com` fails in container

**Possible causes:**
1. Network mode set to wrong value
2. ACL misconfigured
3. DNS resolution failing

**Debug steps:**
```bash
# Check network mode
coi shell
> # Check stderr output for "Network mode: restricted" or "open"

# Check ACL rules
incus network acl list
incus network acl show coi-<container-name>-restricted

# Test DNS resolution
coi run "nslookup example.com"

# Try open mode
coi shell --network=open
> curl example.com  # Should work
```

### Private Networks Are Accessible

**Symptom**: `curl 192.168.1.1` succeeds when it shouldn't

**Possible causes:**
1. Network mode set to "open"
2. ACL not applied to container
3. Container restarted and ACL rules lost

**Debug steps:**
```bash
# Check network mode
coi shell
> # Check stderr for network mode message

# Check ACL is applied
incus config device show <container-name>
# Should see "security.acls: coi-<container-name>-restricted"

# Check ACL rules exist
incus network acl show coi-<container-name>-restricted
# Should see rejection rules for RFC1918 ranges
```

### Host Can't Access Container Services

**Symptom**: Can't access web server running in container

**Possible causes:**
1. Service not listening on 0.0.0.0
2. Wrong IP/port
3. Container firewall blocking ingress (shouldn't happen by default)

**Debug steps:**
```bash
# Check service is listening
coi run "ss -tlnp | grep 8000"

# Get container IP
incus list <container-name>

# Test from host
curl http://<container-ip>:8000

# If still fails, check if container has firewall rules
coi run "iptables -L -n"
```

## Performance Impact

Network isolation using Incus ACLs has minimal performance impact:

- **Overhead**: < 500ms during container setup (ACL creation)
- **Runtime**: No measurable latency impact on network operations
- **Cleanup**: < 100ms during container teardown

ACLs are enforced at kernel level, so packet filtering is highly efficient.

## References

- [Incus Network ACL Documentation](https://linuxcontainers.org/incus/docs/main/howto/network_acls/)
- [RFC1918 - Private Address Space](https://datatracker.ietf.org/doc/html/rfc1918)
- [Cloud Metadata Endpoints (169.254.0.0/16)](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html)
