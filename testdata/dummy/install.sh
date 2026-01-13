#!/bin/bash
# Install dummy CLI into the container for testing
set -e

echo "Installing dummy CLI CLI for testing..."

# Install dummy CLI as /usr/local/bin/claude
cp /workspace/testdata/dummy/claude /usr/local/bin/claude
chmod +x /usr/local/bin/claude

# Verify it works
/usr/local/bin/claude --version

echo "✓ Dummy CLI installed successfully"
