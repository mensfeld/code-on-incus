"""
Integration coverage for the COI_TIMING_DEBUG startup profiler.

These verify the flag actually produces timing DATA end to end — not merely that
it is accepted. A real `coi run` must record `incus` subprocess spans (with
durations) and pipeline phases, dump them as JSON when COI_TIMING_DEBUG_JSON is
set, and render the human timeline on stderr when COI_TIMING_DEBUG is set. When
neither is set, nothing leaks to stderr (the instrumentation is zero-output off).
"""

import json
import os
import subprocess


def _run_true(coi_binary, workspace_dir, env):
    return subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "true"],
        capture_output=True,
        text=True,
        timeout=300,
        env=env,
    )


def test_timing_debug_records_spans_json_and_stderr(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """COI_TIMING_DEBUG(+_JSON) must capture real spans: incus calls and pipeline
    phases with durations in the JSON, and the timeline on stderr."""
    json_path = tmp_path / "timing.json"
    env = os.environ.copy()
    env["COI_TIMING_DEBUG"] = "1"
    env["COI_TIMING_DEBUG_JSON"] = str(json_path)

    result = _run_true(coi_binary, workspace_dir, env)
    assert result.returncode == 0, f"coi run failed: {result.stderr}"

    # --- JSON dump contains real spans with durations ---
    assert json_path.exists(), "COI_TIMING_DEBUG_JSON did not write a file"
    data = json.loads(json_path.read_text())
    assert data.get("total_ms", 0) > 0, f"expected a positive total_ms, got {data.get('total_ms')}"

    records = data.get("records", [])
    assert records, "timing JSON has no records — no timing data was captured"
    for r in records:
        assert "category" in r and "label" in r, f"malformed span: {r}"
        assert r.get("duration_ms", -1) >= 0, f"span without a duration: {r}"

    categories = {r["category"] for r in records}
    # The headline claims: incus subprocesses are timed, and so are pipeline phases.
    assert "incus" in categories, f"no incus spans were recorded (categories: {categories})"
    assert "phase" in categories, (
        f"no pipeline phase spans were recorded (categories: {categories})"
    )
    # At least one incus subprocess took measurable time — i.e. real durations,
    # not all zeros.
    incus_durations = [r["duration_ms"] for r in records if r["category"] == "incus"]
    assert any(d > 0 for d in incus_durations), (
        f"no incus span had a positive duration: {incus_durations}"
    )

    # --- stderr renders the human timeline from the same data ---
    assert "=== coi timing ===" in result.stderr, (
        f"expected the timing timeline on stderr, got:\n{result.stderr[-2000:]}"
    )
    assert "by category:" in result.stderr, "timeline is missing the per-category totals section"
    assert "incus" in result.stderr, "timeline does not mention any incus span"


def test_timing_debug_off_no_stderr_pollution(coi_binary, cleanup_containers, workspace_dir):
    """With neither COI_TIMING_DEBUG nor _JSON set, no timing output reaches stderr."""
    env = os.environ.copy()
    env.pop("COI_TIMING_DEBUG", None)
    env.pop("COI_TIMING_DEBUG_JSON", None)

    result = _run_true(coi_binary, workspace_dir, env)
    assert result.returncode == 0, f"coi run failed: {result.stderr}"
    assert "=== coi timing ===" not in result.stderr, (
        f"timing timeline leaked to stderr when the flag was unset:\n{result.stderr[-2000:]}"
    )
