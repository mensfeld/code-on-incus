# Dummy CLI for Testing

This directory contains a test stub that simulates interactive CLI tool behavior without requiring licenses or authentication.

## Purpose

The dummy CLI allows tests to:
- Run much faster (no CLI startup time or API calls)
- Work without authentication/licenses
- Have predictable, deterministic behavior
- Test container orchestration logic independently

## How It Works

The `dummy` script simulates:
- Initial setup prompts (text style, keyboard shortcuts)
- Session state management
- Resume functionality
- Permission bypass buttons
- Basic interactive chat loop

## Usage in Tests

The `dummy` command is installed in the container's PATH during image build and can be used for testing:

```python
def test_with_dummy(coi_binary):
    """Test using dummy instead of real CLI tool."""
    result = subprocess.run(
        [coi_binary, "shell"],
        ...
    )
```

## Session State

The dummy creates a `.claude` directory structure for compatibility with the coi session management system. This is intentionally kept as `.claude` for now since it's used by the coi tool's session logic.

## Extending

To add more realistic behavior:
1. Parse more CLI flags
2. Simulate specific prompts/responses
3. Add tool use simulation
4. Handle state directory structure more accurately

## Future: Supporting Multiple CLI Tools

This dummy is the first step toward making coi support different CLI tools beyond Claude Code. The generic naming (dummy instead of dummy) reflects this direction.
