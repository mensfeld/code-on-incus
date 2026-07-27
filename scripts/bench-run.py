#!/usr/bin/env python3
"""Benchmark a cold `coi run` and report where the time goes.

Runs `coi run -- true` N times with COI_TIMING_JSON set, then aggregates the
per-span timings across iterations. Everything coi does is an incus subprocess,
so the interesting split is: which incus calls, and how much is coi's own code.

    scripts/bench-run.py                  # 5 iterations of ./coi run -- true
    scripts/bench-run.py -n 3 --bin coi   # 3 iterations of the installed coi
    scripts/bench-run.py -- --profile foo # extra args are passed to coi run

Nothing else must be launching containers at the same time — a concurrent
`incus init` competes for the same disk and inflates every number.
"""

import argparse
import json
import os
import statistics
import subprocess
import sys
import tempfile

# Spans reported on their own line rather than folded into "other incus calls".
HEADLINE_VERBS = ("init", "start", "delete")


def run_once(binary, extra_args, json_path):
    env = dict(os.environ, COI_TIMING_JSON=json_path)
    env.pop("COI_TIMING", None)  # JSON only; keep the terminal quiet
    proc = subprocess.run(
        [binary, "run", *extra_args, "--", "true"],
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr.decode(errors="replace"))
        raise SystemExit(f"coi run failed with exit code {proc.returncode}")
    with open(json_path) as f:
        return json.load(f)


def summarize(dump):
    """Collapse one run into {bucket: seconds}, plus the total."""
    buckets = {}
    for r in dump["records"]:
        if r["category"] != "incus":
            continue
        verb = r["label"].split(" ", 1)[0]
        key = f"incus {verb}" if verb in HEADLINE_VERBS else "incus (other calls)"
        buckets[key] = buckets.get(key, 0) + r["duration_ms"] / 1000
    total = dump["total_ms"] / 1000
    buckets["coi itself (non-subprocess)"] = total - sum(buckets.values())
    return total, buckets


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("-n", "--iterations", type=int, default=5)
    p.add_argument("--bin", default="./coi", help="coi binary to benchmark (default ./coi)")
    p.add_argument("extra", nargs="*", help="extra args for coi run (e.g. --profile foo)")
    args = p.parse_args()

    totals, per_bucket = [], {}
    with tempfile.TemporaryDirectory(prefix="coi-bench-") as tmp:
        for i in range(1, args.iterations + 1):
            dump = run_once(args.bin, args.extra, os.path.join(tmp, f"run{i}.json"))
            total, buckets = summarize(dump)
            totals.append(total)
            for k, v in buckets.items():
                per_bucket.setdefault(k, []).append(v)
            print(f"run {i}/{args.iterations}: {total:.1f}s", file=sys.stderr)

    median_total = statistics.median(totals)
    print(f"\n{args.iterations} runs of `{args.bin} run -- true`")
    print(f"total: median {median_total:.1f}s  min {min(totals):.1f}s  max {max(totals):.1f}s\n")
    print(f"{'median':>9} {'share':>7}  bucket")
    rows = sorted(per_bucket.items(), key=lambda kv: -statistics.median(kv[1]))
    for name, samples in rows:
        med = statistics.median(samples)
        print(f"{med:8.2f}s {med / median_total * 100:6.1f}%  {name}")


if __name__ == "__main__":
    main()
