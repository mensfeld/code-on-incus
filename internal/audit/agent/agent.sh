#!/bin/sh
# coi audit collector — runs inside the sandbox container.
#
# Emits one JSON object per line on stdout, one of:
#   {"ts": "...", "type": "audit",  "msg": "...", "raw": "..."}
#   {"ts": "...", "type": "exec",   "pid": N, "ppid": N, "comm": "...", "args": "..."}
#   {"ts": "...", "type": "net",    "proto": "tcp|udp", "local": "...", "peer": "...", "state": "...", "pid": N, "comm": "..."}
#   {"ts": "...", "type": "file",   "path": "...", "op": "...", "pid": N, "comm": "..."}
#
# Sources, in priority order:
#   1. auditd:        tail -F /var/log/audit/audit.log              -> type=audit + type=file
#   2. syslog/auth:   tail -F /var/log/syslog /var/log/auth.log     -> type=audit (msg=raw line)
#   3. ss snapshots:  ss -tunp every $NET_INTERVAL seconds          -> type=net (new connections only)
#   4. ps snapshots:  ps every $PROC_INTERVAL seconds, diff for new pids -> type=exec
#
# No external deps beyond busybox-compatible coreutils + ss + ps.
# Designed to be cheap: ~5MB RSS, no Python/Perl/awk loops over big files.

set -u

NET_INTERVAL="${COI_AUDIT_NET_INTERVAL:-5}"
PROC_INTERVAL="${COI_AUDIT_PROC_INTERVAL:-2}"

# --- json helpers ------------------------------------------------------------
# Escape a value for inclusion as a JSON string. Handles \, ", control chars.
js() {
    # awk-based escape: portable across busybox awk and gnu awk.
    printf '%s' "$1" | awk '
        BEGIN { RS="\0" }
        {
            gsub(/\\/, "\\\\")
            gsub(/"/,  "\\\"")
            gsub(/\n/, "\\n")
            gsub(/\r/, "\\r")
            gsub(/\t/, "\\t")
            printf "%s", $0
        }'
}

# UTC timestamp with millisecond precision: 2026-05-05T14:30:11.123Z
ts() {
    # date -u +%FT%T.%3NZ on GNU; fall back without ms on busybox.
    d=$(date -u +%FT%T.%3NZ 2>/dev/null) || d=$(date -u +%FT%TZ)
    printf '%s' "$d"
}

emit() {
    # $1 = json body, prepends "ts" and writes one line.
    printf '{"ts":"%s",%s}\n' "$(ts)" "$1"
}

# --- source 1: auditd --------------------------------------------------------
audit_log_path=/var/log/audit/audit.log

start_auditd_tail() {
    [ -r "$audit_log_path" ] || return 1
    (
        tail -n 0 -F "$audit_log_path" 2>/dev/null | while IFS= read -r line; do
            # auditd record format: type=PATH msg=audit(...): ...
            atype=$(printf '%s' "$line" | sed -n 's/^type=\([A-Z_]*\).*/\1/p')
            case "$atype" in
                PATH|CWD)
                    p=$(printf '%s' "$line" | sed -n 's/.* name="\?\([^" ]*\)"\?.*/\1/p')
                    op=$(printf '%s' "$line" | sed -n 's/.* nametype=\([A-Z]*\).*/\1/p')
                    [ -n "$p" ] || continue
                    emit "\"type\":\"file\",\"path\":\"$(js "$p")\",\"op\":\"$(js "$op")\""
                    ;;
                EXECVE|SYSCALL)
                    emit "\"type\":\"audit\",\"msg\":\"$(js "$atype")\",\"raw\":\"$(js "$line")\""
                    ;;
                *)
                    emit "\"type\":\"audit\",\"msg\":\"$(js "$atype")\",\"raw\":\"$(js "$line")\""
                    ;;
            esac
        done
    ) &
    return 0
}

# --- source 2: syslog/auth fallback -----------------------------------------
start_syslog_tail() {
    files=
    for f in /var/log/syslog /var/log/auth.log /var/log/messages; do
        [ -r "$f" ] && files="$files $f"
    done
    [ -n "$files" ] || return 1
    # shellcheck disable=SC2086
    (
        tail -n 0 -F $files 2>/dev/null | while IFS= read -r line; do
            emit "\"type\":\"audit\",\"msg\":\"syslog\",\"raw\":\"$(js "$line")\""
        done
    ) &
    return 0
}

# --- source 3: ss network snapshots ------------------------------------------
prev_ss=
start_ss_loop() {
    command -v ss >/dev/null 2>&1 || return 1
    (
        while :; do
            cur=$(ss -tunp 2>/dev/null | tail -n +2)
            if [ -z "$prev_ss" ]; then
                # first tick: announce existing established connections.
                _diff=$cur
            else
                _diff=$(printf '%s\n' "$cur" | grep -F -v -x -f /tmp/.coi-audit-ss-prev 2>/dev/null || true)
            fi
            printf '%s\n' "$cur" >/tmp/.coi-audit-ss-prev 2>/dev/null
            prev_ss=1
            printf '%s\n' "$_diff" | while IFS= read -r row; do
                [ -n "$row" ] || continue
                # row: "tcp ESTAB 0 0 1.2.3.4:5678 9.8.7.6:443 users:((\"x\",pid=N,fd=N))"
                proto=$(printf '%s' "$row" | awk '{print $1}')
                state=$(printf '%s' "$row" | awk '{print $2}')
                local_a=$(printf '%s' "$row" | awk '{print $5}')
                peer_a=$(printf '%s' "$row" | awk '{print $6}')
                pid=$(printf '%s' "$row" | sed -n 's/.*pid=\([0-9]*\).*/\1/p')
                comm=$(printf '%s' "$row" | sed -n 's/.*users:((\"\([^"]*\)\".*/\1/p')
                emit "\"type\":\"net\",\"proto\":\"$(js "$proto")\",\"state\":\"$(js "$state")\",\"local\":\"$(js "$local_a")\",\"peer\":\"$(js "$peer_a")\",\"pid\":${pid:-0},\"comm\":\"$(js "$comm")\""
            done
            sleep "$NET_INTERVAL"
        done
    ) &
    return 0
}

# --- source 4: process tree snapshots ----------------------------------------
start_ps_loop() {
    command -v ps >/dev/null 2>&1 || return 1
    (
        : >/tmp/.coi-audit-ps-prev
        while :; do
            # capture: pid ppid comm args(rest of line)
            ps -eo pid=,ppid=,comm=,args= 2>/dev/null >/tmp/.coi-audit-ps-cur
            # diff: lines whose first column (pid) is new since last snapshot.
            awk 'NR==FNR{seen[$1]=1; next} !($1 in seen){print}' \
                /tmp/.coi-audit-ps-prev /tmp/.coi-audit-ps-cur | while IFS= read -r row; do
                [ -n "$row" ] || continue
                pid=$(printf '%s' "$row" | awk '{print $1}')
                ppid=$(printf '%s' "$row" | awk '{print $2}')
                comm=$(printf '%s' "$row" | awk '{print $3}')
                args=$(printf '%s' "$row" | awk '{for (i=4;i<=NF;i++) printf "%s%s", $i, (i<NF?" ":"")}')
                emit "\"type\":\"exec\",\"pid\":${pid:-0},\"ppid\":${ppid:-0},\"comm\":\"$(js "$comm")\",\"args\":\"$(js "$args")\""
            done
            mv /tmp/.coi-audit-ps-cur /tmp/.coi-audit-ps-prev
            sleep "$PROC_INTERVAL"
        done
    ) &
    return 0
}

# --- start everything --------------------------------------------------------
sources=
if start_auditd_tail; then sources="$sources auditd"; fi
if [ -z "$sources" ] && start_syslog_tail; then sources="$sources syslog"; fi
if start_ss_loop; then sources="$sources ss"; fi
if start_ps_loop; then sources="$sources ps"; fi

emit "\"type\":\"audit\",\"msg\":\"collector.started\",\"raw\":\"sources:${sources# }\""

# Reap children on signal.
trap 'kill 0 2>/dev/null; exit 0' INT TERM

# Wait forever (children write to stdout).
wait
