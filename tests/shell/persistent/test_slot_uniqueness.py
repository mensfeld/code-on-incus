"""
Meta-test: Verify that all persistent tests use unique slots.

This test validates our test suite itself to ensure no slot conflicts
if tests were to run in the same workspace directory.

This is a safety check to ensure good test hygiene.
"""

import ast
from pathlib import Path


def test_all_persistent_tests_use_unique_slots():
    """
    Meta-test: Verify all persistent test files use unique slot numbers.

    This ensures that if tests accidentally run in the same workspace,
    they won't conflict with each other.
    """

    # Find all test files in the persistent directory
    test_dir = Path(__file__).parent
    test_files = list(test_dir.glob("*.py"))

    # Exclude this meta-test file itself
    test_files = [f for f in test_files if f.name != "test_slot_uniqueness.py"]

    # Track slots per file
    slots_by_file = {}

    for test_file in test_files:
        with open(test_file) as f:
            content = f.read()

        # Parse the Python file
        try:
            tree = ast.parse(content)
        except SyntaxError:
            print(f"Warning: Could not parse {test_file.name}")
            continue

        # Find all slot assignments
        slots = []
        for node in ast.walk(tree):
            # Look for spawn_coi calls with --slot=N
            if isinstance(node, ast.Call) and (
                (isinstance(node.func, ast.Name) and node.func.id == "spawn_coi")
                or (isinstance(node.func, ast.Attribute) and node.func.attr == "spawn_coi")
            ):
                # Look through arguments for list containing slot specification
                for arg in node.args:
                    if isinstance(arg, ast.List):
                        for elt in arg.elts:
                            if (
                                isinstance(elt, ast.Constant)
                                and isinstance(elt.value, str)
                                and "--slot=" in elt.value
                            ):
                                slot_str = elt.value.split("--slot=")[1]
                                try:
                                    slot_num = int(slot_str)
                                    slots.append(slot_num)
                                except ValueError:
                                    pass

        if slots:
            slots_by_file[test_file.name] = sorted(set(slots))

    # Print slot assignments for documentation
    print("\n" + "=" * 70)
    print("Slot assignments by test file:")
    print("=" * 70)
    for filename in sorted(slots_by_file.keys()):
        slots = slots_by_file[filename]
        print(f"  {filename:40s} -> slots {slots}")
    print("=" * 70)

    # Collect all slots
    all_slots = []
    for slots in slots_by_file.values():
        all_slots.extend(slots)

    # Check for duplicates
    unique_slots = set(all_slots)

    if len(all_slots) != len(unique_slots):
        # Find duplicates
        duplicates = []
        for slot in unique_slots:
            if all_slots.count(slot) > 1:
                duplicates.append(slot)

        # Find which files use duplicate slots
        conflicts = []
        for dup_slot in duplicates:
            files_using_slot = [
                filename for filename, slots in slots_by_file.items() if dup_slot in slots
            ]
            conflicts.append(f"Slot {dup_slot} used in: {', '.join(files_using_slot)}")

        conflict_msg = "\n  ".join(conflicts)
        raise AssertionError(f"Duplicate slot assignments found:\n  {conflict_msg}")

    print(
        f"\n✓ All {len(all_slots)} slot assignments are unique across {len(slots_by_file)} test files"
    )
    print(f"  Slots used: {sorted(unique_slots)}")


def test_slot_assignments_are_reasonable():
    """
    Meta-test: Verify slot numbers are in a reasonable range.

    Slots should typically be:
    - >= 1 (slot 0 means auto-allocate)
    - < 100 (to keep test slots separate from typical user slots)
    """

    test_dir = Path(__file__).parent
    test_files = list(test_dir.glob("*.py"))
    test_files = [f for f in test_files if f.name != "test_slot_uniqueness.py"]

    all_slots = []

    for test_file in test_files:
        with open(test_file) as f:
            content = f.read()

        try:
            tree = ast.parse(content)
        except SyntaxError:
            continue

        for node in ast.walk(tree):
            if isinstance(node, ast.Call) and (
                (isinstance(node.func, ast.Name) and node.func.id == "spawn_coi")
                or (isinstance(node.func, ast.Attribute) and node.func.attr == "spawn_coi")
            ):
                for arg in node.args:
                    if isinstance(arg, ast.List):
                        for elt in arg.elts:
                            if (
                                isinstance(elt, ast.Constant)
                                and isinstance(elt.value, str)
                                and "--slot=" in elt.value
                            ):
                                slot_str = elt.value.split("--slot=")[1]
                                try:
                                    slot_num = int(slot_str)
                                    all_slots.append((test_file.name, slot_num))
                                except ValueError:
                                    pass

    # Check each slot
    issues = []
    for filename, slot in all_slots:
        if slot == 0:
            issues.append(f"{filename}: slot 0 (auto-allocate) may cause conflicts in tests")
        elif slot < 1:
            issues.append(f"{filename}: slot {slot} is negative (invalid)")
        elif slot >= 100:
            issues.append(f"{filename}: slot {slot} >= 100 (may conflict with user sessions)")

    if issues:
        issue_msg = "\n  ".join(issues)
        raise AssertionError(f"Slot assignment issues found:\n  {issue_msg}")

    print(f"\n✓ All {len(all_slots)} slot assignments are in reasonable range (1-99)")
