<p align="center">
<img width="256" height="256" src="./assets/logo.png" />
</p>
<h1 align="center">

  termuxcam - Periodic Android Camera Capture for Termux
</h1>

Periodic front-camera capture on Android, running natively in Termux (no
root required), with automatic upload to Telegram and cleanup of the
local file once the upload is confirmed.

Written in Go — a single compiled binary with no external runtime
dependency (Python, Node.js, etc.), designed to run as a long-lived
service via `termux-services`.

**Flow:** capture via `termux-camera-photo` (Termux:API) → upload to
Telegram → delete local image after successful upload.

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

# 3. Determine Front Camera ID

```sh
termux-camera-info
```

Example:

```json
[
  { "id": "0", "facing": "back" },
  { "id": "1", "facing": "front" }
]
```

Update the `cameraID` constant in `main.go` if your front camera ID is
different from `"1"`.

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
> Revoke**, and update it wherever it's configured (step 10.2 below).

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
> delete (`rm context.go`); `main.go` already imports and uses the
> standard `context` package directly.

Verify:

```sh
ls -lh ~/bins/termuxcam
```

------------------------------------------------------------------------

# 8. Test Manually

```sh
export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

~/bins/termuxcam
```

Confirm the photo arrives in Telegram, then `Ctrl+C` to stop.

**Do not skip this step.** If capture or upload fails here, fix it
before wrapping it in a service — a failing service only hides the
error in a log file instead of showing it directly in your terminal.

------------------------------------------------------------------------

# 9. Install termux-services

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

# 10. Install termuxcam Service

## 10.1 Create Service Directory

```sh
mkdir -p ~/.termux/service/termuxcam
mkdir -p ~/.termux/service/termuxcam/log
```

------------------------------------------------------------------------

## 10.2 Create Run Script

```sh
cat <<'EOF' > ~/.termux/service/termuxcam/run
#!/data/data/com.termux/files/usr/bin/sh

# HOME must be set explicitly — processes started by runit do not
# inherit your interactive shell's environment, and the app relies on
# $HOME to resolve where camera_captures/ and its log file live.
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

## 10.3 Enable Service

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

## 10.4 Start Service

```sh
sv up termuxcam
```

Check:

```sh
sv status termuxcam
```

------------------------------------------------------------------------

## 10.5 Monitor Logs

Application log:

```sh
tail -f ~/camera_captures/capture.log
```

Service output:

```sh
tail -f ~/.termux/var/service/termuxcam/log/main/current
```

If the application log never updates even though `sv status` shows the
service running, `$HOME` is likely not resolving inside the service —
double check step 10.2.

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
state, which can take longer than expected. The current `main.go`
uses a 45-second `captureTimeout` and automatically discards and
deletes any file that ends up empty, so this shouldn't recur; if it
still does, increase `captureTimeout` further.

Clean up any pre-existing empty files:

```sh
find ~/camera_captures -name "*.jpg" -size 0 -delete
```

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
`HOME`) — recheck step 10.2, or bypass the issue entirely by hardcoding
the absolute path in `main.go`:

```go
cmd := exec.CommandContext(ctx, "/data/data/com.termux/files/usr/bin/termux-camera-photo", "-c", cameraID, outfile)
```

## TG_BOT_TOKEN / TG_CHAT_ID not set

Check:

```sh
cat ~/.termux/service/termuxcam/run
```

Restart:

```sh
sv restart termuxcam
```

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
| Restart | `sv restart termuxcam` |
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

------------------------------------------------------------------------

# Technical Notes

- Captures every 5 minutes
- Uses front camera
- Discards and deletes any capture that comes back as a 0-byte file
- Uploads using Telegram Bot API
- Deletes files only after successful upload
- Uses Termux wake lock
- Logs to `~/camera_captures/capture.log`
- Runs continuously using `termux-services`
