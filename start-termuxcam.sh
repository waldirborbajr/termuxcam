#!/data/data/com.termux/files/usr/bin/sh

set -eu

PREFIX="${PREFIX:-/data/data/com.termux/files/usr}"
HOME="${HOME:-/data/data/com.termux/files/home}"
PATH="$PREFIX/bin:$PATH"
export HOME PATH

BIN="${TERMUXCAM_BIN:-$HOME/termuxcam/termuxcam}"
CONFIG="${TERMUXCAM_CONFIG:-$HOME/termuxcam/termuxcam.toml}"

if [ ! -x "$BIN" ]; then
    echo "termuxcam executable not found or not executable: $BIN" >&2
    echo "Set TERMUXCAM_BIN to the installed binary path." >&2
    exit 1
fi

if command -v sv >/dev/null 2>&1; then
    exec sv up termuxcam
fi

echo "sv not found; starting termuxcam as a detached daemon."
exec "$BIN" start --config-file "$CONFIG"
