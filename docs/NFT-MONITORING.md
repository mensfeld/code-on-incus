# nftables Network Monitoring for COI

## Overview

COI's nftables-based network monitoring provides **event-driven, kernel-level visibility** into all container network activity. Unlike polling-based monitoring that samples `/proc/net/tcp` every 2 seconds, nftables monitoring captures **every network event** at the kernel level, including short-lived connections, blocked attempts, and DNS queries.

## Architecture

```
Container Network Activity
    ↓
nftables FORWARD chain (kernel)
    ↓ (LOG rules at priority -5/-10)
Kernel logs → journald
    ↓
NFT Daemon (internal/nftmonitor/)
    ├─ JournalReader: Stream kernel logs
    ├─ LogReader: Parse nftables entries
    ├─ NetworkDetector: Analyze threats
    └─ Daemon: Orchestrate + integrate with responder
    ↓
ThreatEvent → Responder (pause/kill) + AuditLog
```

## What It Detects

### Network Threats (Real-Time)

**CRITICAL Level:**
- **Metadata endpoint access** - `169.254.169.254` (cloud provider metadata)
- **Suspicious ports** - 4444, 5555, 1234, 31337, 12345 (common C2/backdoor ports)

**HIGH Level:**
- **RFC1918 private networks** - 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16 (should be blocked by firewall)
- **Allowlist violations** - Connections outside configured allowlist (in allowlist mode)

**WARNING Level:**
- **DNS query anomalies** - Queries to unexpected DNS servers
- **High DNS volume** - >100 queries/minute (potential DNS tunneling)

### Complete Network Visibility

- **Short-lived connections** - HTTP requests <2 seconds that polling would miss
- **Blocked attempts** - Connection attempts rejected by firewall (still logged)
- **DNS queries** - All port 53 traffic (volume and destination servers)
- **All connection attempts** - Even connections that fail immediately

## Installation & Setup

### 1. Install System Dependencies

```bash
# Automated setup (recommended)
./scripts/install-nft-deps.sh

# Manual setup
sudo apt-get install -y libsystemd-dev nftables
sudo usermod -a -G systemd-journal $USER

# Configure passwordless sudo for nft commands
echo '%incus-admin ALL=(ALL) NOPASSWD: /usr/sbin/nft' | sudo tee /etc/sudoers.d/coi-nft
sudo chmod 0440 /etc/sudoers.d/coi-nft
```

**IMPORTANT:** Log out and log back in (or run `newgrp systemd-journal`) for group membership to take effect.

### 2. Verify Setup

```bash
# Run health check
coi health

# Test journal access
journalctl -k -n 10

# Test nftables access
sudo -n nft list ruleset
```

### 3. Required Packages Summary

| Package | Purpose | Required For |
|---------|---------|--------------|
| `libsystemd-dev` | systemd development headers | Building COI with NFT support |
| `nftables` | Kernel packet filtering | Runtime network monitoring |
| `systemd-journal` group | Read kernel logs without sudo | Runtime log access |
| Passwordless sudo (via `/etc/sudoers.d/coi`) | Manage nft rules without prompts | Rule creation/deletion |

## Configuration

### Enable/Disable NFT Monitoring

```toml
# ~/.config/coi/config.toml

[monitoring]
enabled = true                    # Master switch for all monitoring
auto_pause_on_high = true        # Pause container on HIGH threats
auto_kill_on_critical = true     # Kill container on CRITICAL threats

[monitoring.nft]
enabled = true                   # Enable nftables network monitoring (default)
rate_limit_per_second = 100      # Log volume limit for normal traffic
dns_query_threshold = 100        # Alert if >N DNS queries/minute
log_dns_queries = true           # Separate DNS logging (port 53)
lima_host = ""                   # For macOS: "lima-default" (empty for Linux)
```

### Rate Limiting Strategy

NFT monitoring uses **priority-based rate limiting** to prevent log explosion:

1. **Unlimited (Priority -10)** - Always logged:
   - Suspicious traffic (metadata, RFC1918, C2 ports)
   - DNS queries (port 53)

2. **Rate Limited (Priority -5)** - Limited to N packets/second:
   - Normal traffic (default: 100/second)

This ensures critical threats are never missed while preventing performance impact from high-volume benign traffic.

## How It Works

### nftables LOG Rules

When a container starts, COI adds LOG rules to the nftables FORWARD chain:

```bash
# Rule 1: Always log suspicious destinations (no rate limit)
nft add rule ip filter FORWARD priority -10 \
    ip saddr 10.47.62.50 \
    ip daddr { 169.254.169.254, 192.168.0.0/16 } \
    log prefix "NFT_SUSPICIOUS[10.47.62.50]: "

# Rule 2: Always log DNS queries
nft add rule ip filter FORWARD priority -10 \
    ip saddr 10.47.62.50 udp dport 53 \
    log prefix "NFT_DNS[10.47.62.50]: "

# Rule 3: Rate-limited logging for all other traffic
nft add rule ip filter FORWARD priority -5 \
    ip saddr 10.47.62.50 \
    limit rate 100/second \
    log prefix "NFT_COI[10.47.62.50]: "
```

**Key Points:**
- Rules run **before** firewall (priority -5/-10 vs 0+)
- No verdict - packets continue to firewall rules
- Scoped by container IP - only logs that container's traffic
- Unique prefix allows filtering in journald

### Kernel Log Format

```
Feb 9 12:34:56 kernel: NFT_COI[10.47.62.50]: IN=incusbr0 OUT=eth0 SRC=10.47.62.50 DST=8.8.8.8 PROTO=TCP SPT=54321 DPT=53 SYN
```

Parsed into:
```go
NetworkEvent{
    Timestamp:   2026-02-09 12:34:56,
    ContainerIP: "10.47.62.50",
    SrcIP:       "10.47.62.50",
    DstIP:       "8.8.8.8",
    SrcPort:     54321,
    DstPort:     53,
    Protocol:    "TCP",
    Flags:       "SYN",
}
```

### Threat Detection Pipeline

```
1. Network packet → nftables LOG rule
2. Kernel log entry → journald
3. JournalReader streams log (via go-systemd)
4. LogReader parses NFT_* entries → NetworkEvent
5. NetworkDetector analyzes event:
   - isRFC1918(dst) → HIGH
   - dst == 169.254.169.254 → CRITICAL
   - isSuspiciousPort(dpt) → CRITICAL
   - inAllowlist(dst) → HIGH (if violated)
   - analyzeDNSQuery() → WARNING (anomalies)
6. ThreatEvent → Responder → pause/kill + AuditLog
```

## Audit Logging

Network events are logged to:
```
~/.coi/audit/<container-name>-nft.jsonl
```

**Format:** JSON Lines (one JSON object per line)

**Example Entry:**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": "2026-02-09T12:34:56Z",
  "level": "critical",
  "category": "network",
  "title": "Metadata endpoint access",
  "description": "Attempted connection to cloud metadata endpoint",
  "evidence": {
    "timestamp": "2026-02-09T12:34:56Z",
    "container_ip": "10.47.62.50",
    "src_ip": "10.47.62.50",
    "dst_ip": "169.254.169.254",
    "dst_port": 80,
    "src_port": 54321,
    "protocol": "TCP",
    "flags": "SYN"
  },
  "action": "killed"
}
```

### Viewing Audit Logs

```bash
# View all network security events
cat ~/.coi/audit/coi-abc-1-nft.jsonl | jq

# Filter by severity
jq 'select(.level == "critical")' ~/.coi/audit/coi-abc-1-nft.jsonl

# Count events by category
jq -r '.title' ~/.coi/audit/coi-abc-1-nft.jsonl | sort | uniq -c
```

## Comparison: Polling vs nftables

| Feature | Polling (/proc/net/tcp) | nftables + journald |
|---------|-------------------------|---------------------|
| **Short-lived connections** | ❌ Misses <2s connections | ✅ Catches all |
| **Blocked attempts** | ❌ Not visible | ✅ Logged before drop |
| **DNS queries** | ❌ Not visible | ✅ Port 53 traffic |
| **Real-time** | ❌ 2-second delay | ✅ Immediate (ms) |
| **Performance** | ❌ CPU overhead (polling) | ✅ Event-driven |
| **Security** | ⚠️ /proc can be tampered | ✅ Kernel-level |
| **Completeness** | ⚠️ Active connections only | ✅ All attempts |
| **Setup complexity** | ✅ No dependencies | ⚠️ Requires system setup |
| **Log volume** | ✅ Low (snapshots) | ⚠️ Higher (events)* |

*Rate limiting keeps log volume manageable (default: 100 packets/second for normal traffic)

## macOS / Lima Support

NFT monitoring works on macOS via Lima VM. The daemon automatically detects Lima and wraps nft commands:

```go
// Automatic detection
if cfg.NFT.LimaHost != "" {
    cmd = exec.Command("limactl", "shell", cfg.NFT.LimaHost, "sudo", "nft", ...)
} else {
    cmd = exec.Command("sudo", "nft", ...)
}
```

**Configuration:**
```toml
[monitoring.nft]
lima_host = "lima-default"  # Or your Lima instance name
```

## Troubleshooting

### NFT Monitoring Not Starting

**Check health:**
```bash
coi health --format=json | jq '.checks | {nftables, systemd_journal, libsystemd}'
```

**Common Issues:**

1. **nftables not installed**
   ```bash
   sudo apt-get install -y nftables
   ```

2. **No journal access**
   ```bash
   sudo usermod -a -G systemd-journal $USER
   # Log out and log back in
   ```

3. **Sudo password required**
   ```bash
   # Check sudoers file
   sudo cat /etc/sudoers.d/coi
   # Should allow NOPASSWD for nft commands
   ```

4. **libsystemd-dev missing**
   ```bash
   sudo apt-get install -y libsystemd-dev
   # Rebuild COI: go build ./...
   ```

### Rules Not Cleaning Up

If rules persist after session ends:

```bash
# List rules for specific container IP
sudo nft list ruleset | grep "NFT_.*\[10.47.62.50\]"

# Manual cleanup (replace IP)
sudo nft -a list ruleset | grep "NFT_.*\[10.47.62.50\]" | \
    grep -oP 'handle \K\d+' | xargs -I {} sudo nft delete rule ip filter FORWARD handle {}
```

### High Log Volume

If seeing too many log entries:

1. **Reduce rate limit:**
   ```toml
   [monitoring.nft]
   rate_limit_per_second = 50  # Lower from default 100
   ```

2. **Disable DNS logging:**
   ```toml
   [monitoring.nft]
   log_dns_queries = false  # Only suspicious traffic + general
   ```

3. **Check for chatty applications:**
   ```bash
   # View top destination IPs
   sudo journalctl -k | grep "NFT_COI" | grep -oP 'DST=\K[0-9.]+' | sort | uniq -c | sort -rn | head
   ```

## Performance Impact

**CPU Overhead:** <2% (journald streaming is efficient)

**Memory:** ~20MB for daemon process

**Disk I/O:**
- Default: ~100 packets/second logged
- Suspicious traffic: Unlimited (always logged)
- Log rotation handled by systemd

**Latency:** <100ms from network event to threat alert

## Testing

Comprehensive Python integration tests available:

```bash
# Run all NFT monitoring tests
pytest tests/integration/test_nft_monitoring.py -v

# Run specific test class
pytest tests/integration/test_nft_monitoring.py::TestNetworkThreatDetection -v

# Run with detailed output
pytest tests/integration/test_nft_monitoring.py -vvs
```

**Test Coverage:**
- Rule creation and deletion
- Threat detection (metadata, RFC1918, suspicious ports, DNS)
- Audit logging (format, content)
- Daemon lifecycle
- Health checks
- Edge cases (high volume, multiple containers)

## Security Considerations

### Why Kernel-Level Monitoring?

1. **Tamper-proof** - Can't be disabled from inside container
2. **Complete visibility** - Catches all connection attempts (even blocked ones)
3. **Fast connections** - No polling gap - <100ms connections are captured
4. **Defense in depth** - Works even if container process monitoring is compromised

### Threat Model

**Protects Against:**
- ✅ Exfiltration via short HTTP requests
- ✅ DNS tunneling (query volume detection)
- ✅ C2 connections on non-standard ports
- ✅ Metadata endpoint access (cloud credentials)
- ✅ Lateral movement to private networks

**Does Not Protect Against:**
- ❌ Encrypted payload inspection (no DPI)
- ❌ Domain name visibility (only IP addresses and ports)
- ❌ Application-layer attacks (use WAF/IDS)

### Privacy

NFT monitoring only logs:
- Source/destination IP addresses and ports
- Protocol (TCP/UDP/ICMP)
- Flags (SYN, ACK, etc.)

**Not logged:**
- Packet payloads
- Domain names (only resolved IPs)
- HTTP headers or paths
- TLS/SSL encrypted content

## Future Enhancements

Potential future improvements:

1. **DNS payload inspection** - Integrate with systemd-resolved for domain visibility
2. **Connection tracking** - Track full connection lifecycle (SYN → FIN)
3. **Traffic statistics** - Bandwidth usage per destination
4. **Custom rules** - User-defined nftables rules for specific threats
5. **Integration with fail2ban** - Automatic IP blocking for repeated violations

## References

- [nftables Wiki](https://wiki.nftables.org/)
- [systemd Journal Documentation](https://www.freedesktop.org/software/systemd/man/systemd.journal-fields.html)
- [go-systemd Library](https://github.com/coreos/go-systemd)
- [COI Security Monitoring](https://github.com/mensfeld/code-on-incus/wiki/Security-Monitoring)
