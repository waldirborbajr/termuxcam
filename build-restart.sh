#!/bin/bash
# =============================================
# termuxcam - Build + Restart Script
# =============================================

set -euo pipefail  # Exit on error, unset variable, or failed pipe stage

# Resolve the script's own directory so `go build` works regardless of
# where this script is invoked from (cron, Tasker, interactive shell, etc.)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BIN_DIR="$HOME/bins"
SERVICE_DIR="$HOME/.termux/service/termuxcam"
BINARY="$BIN_DIR/termuxcam"
SOURCE_FILE="$SCRIPT_DIR/main.go"

echo "🚀 Starting termuxcam build..."

if ! command -v go >/dev/null 2>&1; then
    echo "❌ Go is not installed or not in PATH."
    exit 1
fi

if [ ! -f "$SOURCE_FILE" ]; then
    echo "❌ Source file not found: $SOURCE_FILE"
    exit 1
fi

mkdir -p "$BIN_DIR"

echo "🔨 Building..."
if ! go build -o "$BINARY" "$SOURCE_FILE"; then
    echo "❌ Build failed!"
    exit 1
fi

echo "✅ Build completed successfully!"
chmod +x "$BINARY"

if [ ! -d "$SERVICE_DIR" ]; then
    echo "⚠️  termuxcam service not found. Run the installation step first."
    exit 1
fi

if ! command -v sv >/dev/null 2>&1; then
    echo "❌ 'sv' command not found. Is termux-services installed and is SVDIR set?"
    exit 1
fi

echo "🔄 Restarting termuxcam service..."
sv restart termuxcam

sleep 2

echo "📊 Service status:"
sv status termuxcam

echo ""
echo "📝 Last log lines:"
tail -n 15 "$HOME/camera_captures/capture.log" 2>/dev/null || echo "Log not yet generated."

echo ""
echo "✅ Done! The service has been rebuilt and restarted."