#!/usr/bin/env python3
"""
Verify that every test directory and standalone test file under tests/ is
referenced by at least one group in the CI integration matrix.

Run: python3 scripts/check-ci-coverage.py
Exit 0 = all covered. Exit 1 = uncovered paths found.
"""
import sys
import yaml
from pathlib import Path

# Files/dirs that intentionally live under tests/ but are not test groups.
NOT_TEST_ENTRIES = {
    "conftest.py",  # pytest fixtures shared across groups
    "support",      # helper modules (helpers.py etc.)
    "__pycache__",
}


def main() -> int:
    repo = Path(__file__).resolve().parent.parent
    ci_file = repo / ".github" / "workflows" / "ci.yml"
    tests_dir = repo / "tests"

    with open(ci_file) as f:
        ci = yaml.safe_load(f)

    # Collect every path token from every group's `path:` field.
    covered: set[str] = set()
    for group in ci["jobs"]["integration"]["strategy"]["matrix"]["test_group"]:
        path_field = group.get("path", "")
        for token in path_field.split():
            covered.add(token)

    uncovered: list[str] = []

    for item in sorted(tests_dir.iterdir()):
        if item.name in NOT_TEST_ENTRIES or item.name.startswith("_"):
            continue

        if item.is_dir():
            # Only flag directories that actually contain .py files.
            if not any(item.rglob("*.py")):
                continue
            rel = f"tests/{item.name}"
        elif item.suffix == ".py":
            rel = f"tests/{item.name}"
        else:
            continue

        # Covered if: exact match, rel is a sub-path of a covered token,
        # or a covered token is a sub-path of rel (e.g. tests/shell covered
        # via tests/shell/ephemeral and tests/shell/persistent).
        is_covered = any(
            rel == p
            or rel.startswith(p.rstrip("/") + "/")
            or p.startswith(rel.rstrip("/") + "/")
            for p in covered
        )
        if not is_covered:
            uncovered.append(rel)

    if uncovered:
        print("ERROR: the following test paths are not assigned to any CI group:")
        for p in uncovered:
            print(f"  {p}")
        print()
        print("Add them to a group in .github/workflows/ci.yml under")
        print("jobs.integration.strategy.matrix.test_group[*].path")
        return 1

    print(f"OK: all test paths are covered ({len(covered)} path entries across CI groups)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
