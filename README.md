<p align="center">
<img width="256" height="256" src="./assets/logo.png" />
</p>
<h1 align="center">

  termuxcam - Periodic Android Camera Capture for Termux
</h1>

Periodic camera capture on Android, running natively in Termux (no
root required), with automatic upload to Telegram and cleanup of the
local file once the upload is confirmed.

Written in Go — a single compiled binary with no external runtime
dependency (Python, Node.js, etc.), designed to run as a long-lived
service via `termux-services`. Capture interval and camera selection
(front/back/both) are configured at runtime via a plain-text
`termuxcam.conf` file — no recompiling needed to change them.

**Flow:** capture via `termux-camera-photo` (Termux:API) → upload to
Telegram with a caption identifying which camera → delete local image
after successful upload.

------------------------------------------------------------------------

# Prerequisites

- Android device
- Termux (F-Droid version recommended)
- Termux:API Android application installed **and opened at least once**
- `termux-api` package installed
- Internet connection
- Telegram account

------------------------------------------------------------------------

# 1. Install Go

```sh
pkg update && pkg upgrade -y
pkg install golang git
```

Verify:

```sh
go version
```

------------------------------------------------------------------------

# 2. Install Termux:API Dependencies

`termuxcam` requires the `termux-camera-photo` command provided by the
`termux-api` package.

Install:

```sh
pkg install termux-api
```

Install the **Termux:API Android application** from F-Droid (same source
as Termux itself — mixing a Play Store build of one with an F-Droid
build of the other is a common cause of broken communication between
them).

Both components are required:

| Component | Purpose |
|---|---|
| `termux-api` package | Provides commands like `termux-camera-photo` |
| Termux:API Android app | Provides Android hardware access and permissions |

Open the Termux:API application once (it has no real UI — opening it
just wakes up its background process).

Verify:

```sh
which termux-camera-photo
```

Expected:

```text
/data/data/com.termux/files/usr/bin/termux-camera-photo
```

### Grant Camera permission explicitly

Go to:

```
Android Settings → Apps → Termux:API → Permissions → Camera
```

Set it to **Allow** / **Allow always** — not "Ask every time" and not
"Allow only while using the app". Termux:API is invoked headlessly from
the terminal with no dialog to respond to, so either of those two
settings will make camera calls **hang forever with no error message**.
This is the single most common cause of `termux-camera-photo` silently
freezing.

Also disable battery optimization for **Termux:API itself** (not just
Termux):

```
Android Settings → Apps → Termux:API → Battery → Unrestricted
```

### Sanity check before moving on

```sh
timeout 15 termux-camera-photo -c 1 ~/test.jpg
echo "exit code: $?"
ls -lh ~/test.jpg
```

- Exit code `124` means it timed out — go back and check the Camera
  permission setting above.
- Exit code `0` with a file **larger than 0 bytes** means you're good
  to proceed.

If you're unsure whether the issue is camera-specific or a broader
Termux↔Termux:API communication problem, test with a lighter command
first:

```sh
termux-battery-status
```

If this returns a JSON block with battery info, the app communication
channel itself is fine and the problem is isolated to the Camera
permission.

------------------------------------------------------------------------

# 3. Determine Your Device's Physical Camera IDs

`termux-camera-photo -c <id>` expects a raw hardware ID, and **this ID
is device-specific** — it is not safe to assume `0` is the back camera
and `1` is the front camera. Confirm it explicitly:

```sh
termux-camera-info
```

Example output:

```json
[
  { "id": "0", "facing": "back" },
  { "id": "1", "facing": "front" }
]
```

`main.go` keeps these as two clearly labeled constants near the top of
the file, decoupled from everything else in the program:

```go
const (
	frontCameraHWID = "1"
	backCameraHWID  = "0"
)
```

If your device reports different IDs than the example above, edit
these two constants before building (step 7) — nothing else in the
code needs to change, since the rest of the program only ever refers
to cameras by the semantic labels `"front"` / `"back"`, never by raw
ID.

------------------------------------------------------------------------

# 4. Clone Repository

```sh
git clone https://github.com/waldirborbajr/termuxcam.git
cd termuxcam
```

------------------------------------------------------------------------

# 5. Create Telegram Bot

1. Open Telegram.
2. Contact **@BotFather**.
3. Send:
   ```text
   /newbot
   ```
4. Follow instructions.
5. Save the generated bot token.

Example:

```text
123456789:ABCDEFxxxxxxxxxxxxxxxx
```

> ⚠️ Treat this token like a password. Never paste it into a chat, issue,
> or commit history in plain text — if it's ever exposed, revoke it
> immediately via **@BotFather → /mybots → your bot → API Token →
> Revoke**, and update it wherever it's configured (step 11.2 below).

------------------------------------------------------------------------

# 6. Get Telegram Chat ID

Send a message to your bot first.

Open:

```text
https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates
```

Find:

```json
"chat": { "id": 782816475 }
```

Save:

- `TG_BOT_TOKEN`
- `TG_CHAT_ID`

------------------------------------------------------------------------

# 7. Build

Create a binary directory:

```sh
mkdir -p ~/bins
```

Build:

```sh
go build -o ~/bins/termuxcam main.go
```

> Note: the project is a single `main.go` file — there is no separate
> `context.go` to pass to `go build`. If your local copy still has one
> left over from an earlier version, it's unused dead code and safe to
> delete (`rm context.go`).

Verify:

```sh
ls -lh ~/bins/termuxcam
```

------------------------------------------------------------------------

# 8. Configure: `termuxcam.conf`

`termuxcam` reads its settings from a `termuxcam.conf` file placed
**in the same directory as the binary** (`~/bins/`, if you followed
step 7 as-is). This lets you change the capture interval or which
camera(s) to use without recompiling.

If the file doesn't exist yet, the app creates a default one
automatically on first run — you don't have to create it by hand. To
set it up ahead of time instead:

```sh
cat <<'EOF' > ~/bins/termuxcam.conf
# termuxcam configuration

# Capture interval — a number followed by 'm' (minutes) or 'h' (hours)
# capture=15m   capture=45m   capture=2h   capture=3h
capture=5m

# Camera mode:
#   0 = back only
#   1 = front only (default)
#   2 = both (captures and uploads one photo from each camera per cycle)
camera=1
EOF
```

| Setting | Accepted values | Notes |
|---|---|---|
| `capture` | `<number>m` or `<number>h` | Clamped to a safe range: 1 minute minimum, 24 hours maximum. Out-of-range or malformed values are ignored and the previous/default value is kept, with a warning in the log. |
| `camera` | `0`, `1`, or `2` | `0`=back, `1`=front, `2`=both. Anything else is ignored and the previous/default value is kept, with a warning in the log. |

Inline comments are supported (`capture=3h  # every 3 hours`), and
malformed or unknown lines are skipped with a warning rather than
crashing the app.

**Editing the config while the service is running:** changes only take
effect after a restart — the file is read once at startup, not
watched live.

```sh
sv restart termuxcam
```

------------------------------------------------------------------------

# 9. Test Manually

```sh
export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

~/bins/termuxcam
```

Confirm the photo (or photos, if `camera=2`) arrives in Telegram with
the expected caption (`Front camera: ...` / `Back camera: ...`), then
`Ctrl+C` to stop.

**Do not skip this step.** If capture or upload fails here, fix it
before wrapping it in a service — a failing service only hides the
error in a log file instead of showing it directly in your terminal.

------------------------------------------------------------------------

# 10. Install termux-services

```sh
pkg install termux-services
```

Close and reopen Termux after this (it needs to restart its `init`
process). Then load the environment:

```sh
source $PREFIX/etc/profile.d/start-services.sh
```

Verify:

```sh
which sv
```

------------------------------------------------------------------------

# 11. Install termuxcam Service

## 11.1 Create Service Directory

```sh
mkdir -p ~/.termux/service/termuxcam
mkdir -p ~/.termux/service/termuxcam/log
```

------------------------------------------------------------------------

## 11.2 Create Run Script

```sh
cat <<'EOF' > ~/.termux/service/termuxcam/run
#!/data/data/com.termux/files/usr/bin/sh

# HOME must be set explicitly — processes started by runit do not
# inherit your interactive shell's environment.
export HOME=/data/data/com.termux/files/home
export PATH=/data/data/com.termux/files/usr/bin:$PATH

export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

exec /data/data/com.termux/files/home/bins/termuxcam
EOF
```

Make executable:

```sh
chmod +x ~/.termux/service/termuxcam/run
```

------------------------------------------------------------------------

## 11.3 Enable Service

Create the symbolic link used by `runit`:

```sh
ln -s ~/.termux/service/termuxcam $PREFIX/var/service/termuxcam
```

Verify:

```sh
ls -la $PREFIX/var/service
```

Expected:

```text
termuxcam -> /data/data/com.termux/files/home/.termux/service/termuxcam
```

------------------------------------------------------------------------

## 11.4 Start Service

```sh
sv up termuxcam
```

Check:

```sh
sv status termuxcam
```

------------------------------------------------------------------------

## 11.5 Monitor Logs

Application log (includes the loaded config on every startup, e.g.
`Config loaded -> interval=3h | cameraMode=2 -> front(hw=1), back(hw=0) | config=...`):

```sh
tail -f ~/camera_captures/capture.log
```

Service output:

```sh
tail -f ~/.termux/var/service/termuxcam/log/main/current
```

If the application log never updates even though `sv status` shows the
service running, `$HOME` is likely not resolving inside the service —
double check step 11.2.

------------------------------------------------------------------------

# Troubleshooting

## termux-camera-photo hangs with no error and never returns

This is almost always the Camera permission being set to "Ask every
time" or "Allow only while using the app" — see the **Sanity check**
box in step 2. There is no dialog to respond to in a headless terminal
call, so the process just waits forever. Set the permission to **Allow
always** and retest with:

```sh
timeout 15 termux-camera-photo -c 1 ~/test.jpg
echo "exit code: $?"
```

## Captured files exist but are 0 bytes

The capture was killed mid-write by the timeout before
`termux-camera-photo` finished writing the file — typically on the
first call after Termux:API's background process was woken from a cold
state, which can take longer than expected. `main.go` uses a
45-second `captureTimeout` and automatically discards and deletes any
file that ends up empty, so this shouldn't recur; if it still does,
increase `captureTimeout` in `main.go` and rebuild.

Clean up any pre-existing empty files:

```sh
find ~/bins/camera_captures -name "*.jpg" -size 0 -delete
```

## Photos are labeled with the wrong camera, or `camera=1` captures the wrong physical camera

This means the two hardware ID constants don't match your device. Redo
step 3 (`termux-camera-info`) and confirm `frontCameraHWID` /
`backCameraHWID` in `main.go` match what your device actually reports,
then rebuild. The rest of the program only works with the semantic
`"front"`/`"back"` labels, so fixing these two constants is the only
change needed.

## termux-camera-photo executable not found

Error:

```text
exec: "termux-camera-photo": executable file not found in $PATH
```

Install:

```sh
pkg install termux-api
```

Verify:

```sh
which termux-camera-photo
```

If `PATH` looks correct in your interactive shell but the service still
can't find the binary, the `run` script may not be exporting `PATH` (or
`HOME`) — recheck step 11.2.

## TG_BOT_TOKEN / TG_CHAT_ID not set

Check:

```sh
cat ~/.termux/service/termuxcam/run
```

Restart:

```sh
sv restart termuxcam
```

## Config changes don't seem to apply

The config file is only read once, at process startup. After editing
`~/bins/termuxcam.conf`, restart the service:

```sh
sv restart termuxcam
```

Check the log line right after startup to confirm what was actually
loaded — it prints the resolved interval and camera(s) explicitly.

## Service won't restart / seems stuck on an old process

`sv restart` sends `SIGTERM` and waits — if the running process is
blocked inside a hung system call (e.g. `termux-camera-photo` itself
hanging per the permission issue above), it may not exit in time and
`runsv` gives up, leaving the old process alive. Check for it directly:

```sh
ps aux | grep termuxcam
```

If a stale process is still running after `sv down termuxcam`, force it:

```sh
sv down termuxcam
sleep 2
pkill -9 -f termuxcam
sv up termuxcam
```

## Service directory not found

Recreate the symbolic link:

```sh
ln -s ~/.termux/service/termuxcam $PREFIX/var/service/termuxcam
sv up termuxcam
```

------------------------------------------------------------------------

# Service Management

| Action | Command |
|---|---|
| Start | `sv up termuxcam` |
| Stop | `sv down termuxcam` |
| Restart (also reloads config) | `sv restart termuxcam` |
| Status | `sv status termuxcam` |
| Force-kill a stuck process | `pkill -9 -f termuxcam` |

------------------------------------------------------------------------

# Battery Optimization

Disable Android battery restrictions for **both** apps, or the service
may get suspended in the background:

```
Settings → Apps → Termux → Battery → Unrestricted
Settings → Apps → Termux:API → Battery → Unrestricted
```

On MIUI (Xiaomi) devices, this alone is often not enough — also check:

- **Security app → Battery → Autostart** — enable for Termux and
  Termux:API
- **Security app → Battery → App battery saver** — set Termux and
  Termux:API (grouped together as "Termux user") to **No restrictions**
- **Recent apps → hold the Termux icon → lock (padlock icon)** — protects
  it from being killed by "Clear all" in the recents screen

------------------------------------------------------------------------

# Technical Notes

- Capture interval and camera selection are configured via
  `termuxcam.conf`, read once at startup from the same directory as
  the binary — no recompilation needed to change them
- Camera mode: `0`=back, `1`=front, `2`=both (captures and uploads a
  separate photo per camera each cycle)
- Physical hardware camera IDs are isolated in two constants in
  `main.go` and are device-specific — verify with `termux-camera-info`
- Discards and deletes any capture that comes back as a 0-byte file
- Uploads using the Telegram Bot API, with a caption identifying the
  camera and timestamp
- Deletes files only after successful upload; keeps them locally on
  upload failure for manual retry
- Uses Termux wake lock (skipped gracefully with a log warning if
  `termux-wake-lock` isn't available)
- All output (photos, log) lives in `camera_captures/` next to the
  binary
- Logs to `<binary dir>/camera_captures/capture.log`
- Runs continuously using `termux-services`, restarting automatically
  if the process dies
