# Building Test Images with Dummy CLI

## Better Approach: Install Dummy CLI in Container

Instead of modifying PATH on the host, we should build a test image with dummy CLI pre-installed.

### Option 1: Build Custom Image with Dummy CLI

```bash
# Create build script that installs dummy CLI
cat > testdata/dummy/install.sh << 'EOF'
#!/bin/bash
set -e

# Install dummy CLI as /usr/local/bin/claude
cp /workspace/testdata/dummy/claude /usr/local/bin/claude
chmod +x /usr/local/bin/claude

echo "Dummy CLI installed successfully"
EOF

# Build custom image
coi build custom coi-test-dummy \
    --script testdata/dummy/install.sh

# Now tests can use this image
coi shell --image coi-test-dummy
```

### Option 2: Mount Dummy CLI via .claude Directory

Create a `.claude` directory structure that includes the dummy CLI binary:

```bash
# In test setup
test_claude_dir="${workspace}/.claude"
mkdir -p "${test_claude_dir}/bin"
cp testdata/dummy/claude "${test_claude_dir}/bin/claude"
chmod +x "${test_claude_dir}/bin/claude"

# COI will mount this and container will use it
coi shell --workspace "${workspace}"
```

### Option 3: Use Fixture to Prepare Test Image (RECOMMENDED)

```python
@pytest.fixture(scope="session")
def dummy_image(coi_binary):
    """Build a test image with dummy CLI pre-installed."""

    image_name = "coi-test-dummy"

    # Check if image already exists
    result = subprocess.run(
        [coi_binary, "image", "exists", image_name],
        capture_output=True
    )

    if result.returncode == 0:
        return image_name  # Already built

    # Build image with dummy CLI
    script_path = "testdata/dummy/install.sh"
    result = subprocess.run(
        [coi_binary, "build", "custom", image_name,
         "--script", script_path],
        capture_output=True,
        text=True,
        timeout=300
    )

    if result.returncode != 0:
        pytest.skip(f"Could not build dummy CLI image: {result.stderr}")

    return image_name


# In tests
def test_with_dummy(coi_binary, dummy_image, workspace_dir):
    """Test using pre-built image with dummy CLI."""

    child = spawn_coi(
        coi_binary,
        ["shell", "--image", dummy_image],
        cwd=workspace_dir
    )

    # Dummy CLI is already in the container!
    wait_for_prompt(child)
    # ...
```

## Why This is Better

### Current Approach (PATH modification):
- ❌ Modifies host environment
- ❌ Doesn't test actual installation
- ❌ Different from production setup

### New Approach (Image with dummy CLI):
- ✅ Dummy CLI inside container (realistic)
- ✅ Tests actual mount/installation mechanisms
- ✅ Closer to production setup
- ✅ No PATH manipulation needed
- ✅ Reusable across tests (build once)

## Implementation

The recommended approach is **Option 3** with a session-scoped fixture that builds the image once and reuses it for all tests.
