#!/bin/bash
# Install dummy CLI into the container for testing
set -e

echo "Installing dummy CLI for testing..."

# The dummy file should be available at /tmp/dummy (pushed by buildCustom)
# If not available, try to download from workspace (for manual testing)
if [ -f "/tmp/dummy" ]; then
    echo "Using dummy from /tmp/dummy"
    cp /tmp/dummy /usr/local/bin/claude
elif [ -f "/workspace/testdata/dummy/dummy" ]; then
    echo "Using dummy from workspace"
    cp /workspace/testdata/dummy/dummy /usr/local/bin/claude
else
    echo "Error: dummy file not found in /tmp or /workspace"
    exit 1
fi

chmod +x /usr/local/bin/claude

# Verify it works
/usr/local/bin/claude --version

echo "✓ Dummy CLI installed successfully"
