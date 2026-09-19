"""
Regression test for #777: `coi build` must succeed on a non-default `[incus]
project`.

The bug: after building and publishing the sandbox image, `getImageFingerprint`
looked the freshly-published image up with a hardcoded `--project default`. On a
project other than "default" the publish landed in the configured project but the
fingerprint lookup queried "default", so the build aborted with "image not found"
*immediately after a successful publish*. The fix routes the lookup through
`container.IncusProject` like every other image operation.

To reproduce the bug specifically, the test project must have its OWN image store
(`features.images=true`): otherwise it shares the default project's images and a
lookup against "default" would still find the image, masking the bug. We stage the
already-built `coi-default` base into the project (via `incus image export` +
`import`, since `incus image copy --target-project` is a network operation) and
run a trivial *custom* build there (fast — the base exists, the script is a
no-op), which still exercises the publish + fingerprint path where #777 lived.

Deterministic coverage of the same wiring lives in the incus-less unit job
(`internal/image`, TestImageFingerprintListArgs_UsesConfiguredProject); this test
is the end-to-end proof against a real Incus.
"""

import os
import subprocess

import pytest

from support.helpers import is_incus_permission_error

BASE_ALIAS = "coi-default"  # image.CoiAlias — the default sandbox base


def _incus(*args, timeout=120):
    return subprocess.run(["incus", *args], capture_output=True, text=True, timeout=timeout)


def test_build_succeeds_on_nondefault_project(coi_binary, tmp_path):
    # The custom build launches from the coi-default base; skip if it's absent.
    if _incus("image", "info", BASE_ALIAS).returncode != 0:
        pytest.skip(f"{BASE_ALIAS} base image not present")

    project = f"coi-test-proj-{os.urandom(4).hex()}"
    image_name = "coi-test-nondefault-project"

    # A project with its own image store — required to reproduce #777 (a shared
    # image store would let a lookup against "default" find the image anyway).
    create = _incus(
        "project",
        "create",
        project,
        "-c",
        "features.images=true",
        "-c",
        "features.profiles=false",
    )
    if create.returncode != 0:
        if is_incus_permission_error(create.stderr):
            pytest.skip(f"No permission to create incus project: {create.stderr}")
        pytest.fail(f"Failed to create project {project}: {create.stderr}")

    try:
        # The custom build needs its base visible in the project's own image
        # store. `incus image copy … --target-project` performs a
        # server-to-server (network) copy that fails on a socket-only daemon with
        # "The source server isn't listening on the network", so the base is
        # staged via export + import instead. Export yields either a single
        # unified tarball (base.tar.gz — how coi's published images export) or a
        # split metadata+rootfs pair (base + base.root); import takes the
        # metadata first, then the optional rootfs.
        export_dir = tmp_path / "imgexport"
        export_dir.mkdir()
        exp = _incus(
            "image",
            "export",
            BASE_ALIAS,
            str(export_dir / "base"),
            "--project",
            "default",
            timeout=180,
        )
        if exp.returncode != 0:
            if is_incus_permission_error(exp.stderr):
                pytest.skip(f"No permission to export base image: {exp.stderr}")
            pytest.fail(f"Failed to export base image: {exp.stderr}")
        # metadata (no .root suffix) sorts before the optional rootfs tarball.
        parts = sorted(export_dir.iterdir(), key=lambda p: p.name.endswith(".root"))
        imp = _incus(
            "image",
            "import",
            *[str(p) for p in parts],
            "--project",
            project,
            "--alias",
            BASE_ALIAS,
            timeout=180,
        )
        if imp.returncode != 0:
            if is_incus_permission_error(imp.stderr):
                pytest.skip(f"No permission to import base image: {imp.stderr}")
            pytest.fail(f"Failed to import base into {project}: {imp.stderr}")

        # Select the non-default project via trusted-scope project config, and a
        # trivial custom build (no-op script) that still publishes + fingerprints.
        coi_dir = tmp_path / ".coi"
        (coi_dir / "profiles" / "nondefault").mkdir(parents=True)
        (coi_dir / "config.toml").write_text(f'[incus]\nproject = "{project}"\n')
        (coi_dir / "profiles" / "nondefault" / "config.toml").write_text(
            f'[container]\nimage = "{image_name}"\n\n'
            f'[container.build]\nbase = "{BASE_ALIAS}"\nscript = "build.sh"\n'
        )
        (coi_dir / "profiles" / "nondefault" / "build.sh").write_text("#!/bin/bash\nset -e\ntrue\n")

        result = subprocess.run(
            [coi_binary, "build", "--profile", "nondefault"],
            capture_output=True,
            text=True,
            timeout=300,
            cwd=str(tmp_path),
        )
        # Pre-fix, this fails with "image not found: <image>" right after publish.
        assert result.returncode == 0, (
            "coi build must succeed on a non-default [incus] project (#777). "
            f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}"
        )

        # The image really landed in the configured project's store.
        check = _incus("image", "info", image_name, "--project", project)
        assert check.returncode == 0, (
            f"Built image should exist in project {project}. stderr:\n{check.stderr}"
        )
    finally:
        _incus("image", "delete", image_name, "--project", project)
        _incus("image", "delete", BASE_ALIAS, "--project", project)
        _incus("project", "delete", project)
