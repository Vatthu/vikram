#!/bin/bash
# Install LeVik as a macOS launchd service.
# The gateway starts on boot and restarts automatically on crash.
set -e

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
LEVIK_HOME="${LEVIK_HOME:-$HOME/.levik}"
PLIST="com.levik.team.plist"

echo "=== LeVik Daemon Install ==="
echo "Install dir: $INSTALL_DIR"
echo "LeVik home:  $LEVIK_HOME"

# Create required directories
mkdir -p "$LEVIK_HOME/logs" "$LEVIK_HOME/workspace"

# Build and install the binary
echo ""
echo "Building levik..."
make build
cp build/levik "$INSTALL_DIR/levik"
echo "✓ Binary installed to $INSTALL_DIR/levik"

# Update the plist with the current user's paths
TMP_PLIST=$(mktemp)
sed "s|/Users/levik|$HOME|g; s|/usr/local/bin/levik|$INSTALL_DIR/levik|g" contrib/$PLIST > "$TMP_PLIST"

# Install launchd plist
LAUNCHD_DIR="$HOME/Library/LaunchAgents"
mkdir -p "$LAUNCHD_DIR"
cp "$TMP_PLIST" "$LAUNCHD_DIR/$PLIST"
rm "$TMP_PLIST"
echo "✓ LaunchAgent installed to $LAUNCHD_DIR/$PLIST"

# Load the service
launchctl unload "$LAUNCHD_DIR/$PLIST" 2>/dev/null || true
launchctl load "$LAUNCHD_DIR/$PLIST"
echo "✓ Service loaded"

echo ""
echo "=== LeVik daemon is running ==="
echo "Logs:   $LEVIK_HOME/logs/gateway.log"
echo "Status: launchctl list | grep levik"
echo "Stop:   launchctl unload $LAUNCHD_DIR/$PLIST"
