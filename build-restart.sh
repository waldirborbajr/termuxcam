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
# termuxcam is split across multiple .go files in the same package
# (main.go, config.go, state.go, camera.go, capture.go, telegram.go,
# system.go, logger.go) — build the whole directory, not a single file.
SOURCE_DIR="$SCRIPT_DIR"
# The running binary writes its output next to itself (outputDir is
# resolved relative to os.Executable(), not $HOME) — keep this in sync
# with BIN_DIR above, since that's where the binary actually lives.
LOG_FILE="$BIN_DIR/camera_captures/capture.log"

echo "🚀 Starting termuxcam build..."

if ! command -v go >/dev/null 2>&1; then
    echo "❌ Go is not installed or not in PATH."
    exit 1
fi

if [ ! -f "$SOURCE_DIR/main.go" ]; then
    echo "❌ main.go not found in: $SOURCE_DIR"
    exit 1
fi

mkdir -p "$BIN_DIR"

echo "🔨 Building..."
if ! go build -o "$BINARY" "$SOURCE_DIR"; then
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
tail -n 15 "$LOG_FILE" 2>/dev/null || echo "Log not yet generated."

echo ""
echo "✅ Done! The service has been rebuilt and restarted."
