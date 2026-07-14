# termuxcam

<p align="center">
  <img width="256" height="256" src="./assets/logo.png" />
</p>

<h1 align="center">termuxcam - Periodic Android Camera Capture for Termux</h1>

Periodic front-camera capture on Android, running natively in Termux (no root required), with automatic upload to Telegram and cleanup of the local file once the upload is confirmed.

Written in Go — a single compiled binary with no external runtime dependency (Python, Node.js, etc.), designed to run as a long-lived service via `termux-services`.

**Flow:** capture via `termux-camera-photo` (Termux:API) → upload to Telegram → delete local image after successful upload.

---

## Prerequisites

- Android device
- Termux (F-Droid version recommended)
- Termux:API application installed
- Internet connection
- Telegram account

---

## 1. Install Go

```sh
pkg update && pkg upgrade -y
pkg install golang git
```

Verify:

```sh
go version
```

---

## 2. Install Termux:API

Install the CLI package:

```sh
pkg install termux-api
```

Then:

1. Install the **Termux:API** Android application from F-Droid.
2. Open the application once.
3. Grant Camera permission.

Verify:

```sh
termux-camera-info
```

---

## 3. Determine the Front Camera ID

```sh
termux-camera-info
```

If your front camera is not `1`, update the `cameraID` constant in `main.go`.

---

## 4. Clone the Repository

```sh
git clone https://github.com/waldirborbajr/termuxcam.git
cd termuxcam
```

---

## 5. Create a Telegram Bot

1. Open Telegram.
2. Start a conversation with **@BotFather**.
3. Send `/newbot`.
4. Follow the instructions.
5. Save the bot token.

---

## 6. Obtain Your Telegram Chat ID

Send any message to your bot first.

Open:

```text
https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates
```

Locate:

```json
"chat": {
  "id": 782816475
}
```

Save:

- `TG_BOT_TOKEN`
- `TG_CHAT_ID`

---

## 7. Build

Create a personal binaries directory:

```sh
mkdir -p ~/bins
```

Build directly into the binaries directory:

```sh
go build -o ~/bins/termuxcam main.go context.go
```

Verify:

```sh
ls -lh ~/bins/termuxcam
```

---

## 8. Test Manually

```sh
export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

~/bins/termuxcam
```

Confirm that photos arrive in Telegram.

---

## 9. Install termux-services

```sh
pkg install termux-services
source $PREFIX/etc/profile.d/start-services.sh
```

Verify:

```sh
which sv
```

---

## 10. Install termuxcam as a Service

### 10.1 Create the Service Directory

```sh
mkdir -p ~/.termux/service/termuxcam
mkdir -p ~/.termux/service/termuxcam/log
```

### 10.2 Create the Run Script

```sh
cat <<'EOF' > ~/.termux/service/termuxcam/run
#!/data/data/com.termux/files/usr/bin/sh

export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

#exec /data/data/com.termux/files/home/bins/termuxcam
exec /data/data/com.termux/files/home/bins/termuxcam \
    >> /data/data/com.termux/files/home/termuxcam.log 2>&1
EOF
```

```sh
chmod +x ~/.termux/service/termuxcam/run
```

### 10.3 Enable the Service

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

### 10.4 Start the Service

```sh
sv up termuxcam
```

Check:

```sh
sv status termuxcam
```

### 10.5 View Logs

```sh
tail -f ~/.termux/var/service/termuxcam/log/main/current
```

---

## 11. Verify Automatic Startup

```sh
sv status termuxcam
```

The service should still be running after restarting Termux or rebooting the device.

---

## 12. Disable Battery Optimization

Set:

**Settings → Apps → Termux → Battery → Unrestricted**

---

## 13. Shared Storage Access (Optional)

```sh
termux-setup-storage
```

---

## Service Management

| Action | Command |
|----------|----------|
| Start | `sv up termuxcam` |
| Stop | `sv down termuxcam` |
| Restart | `sv restart termuxcam` |
| Status | `sv status termuxcam` |
| Disable | `sv-disable termuxcam` |
| View logs | `tail -f ~/.termux/var/service/termuxcam/log/main/current` |

---

## Troubleshooting

### unable to change to service directory: file does not exist

Verify:

```sh
ls -la $PREFIX/var/service
```

If `termuxcam` is missing, create the symbolic link:

```sh
ln -s ~/.termux/service/termuxcam $PREFIX/var/service/termuxcam
```

Then:

```sh
sv up termuxcam
```

### permission denied

```sh
chmod +x ~/bins/termuxcam
chmod +x ~/.termux/service/termuxcam/run
```

### No photos arrive in Telegram

```sh
curl https://api.telegram.org/bot$TG_BOT_TOKEN/getMe
curl https://api.telegram.org/bot$TG_BOT_TOKEN/getUpdates
```

### Camera capture fails

```sh
termux-camera-info
```

Ensure the Termux:API application has Camera permission.

---

## Technical Notes

- Captures an image every 5 minutes
- Uses the front camera by default
- Uploads via Telegram Bot API `sendPhoto`
- Deletes images only after successful upload
- Acquires a wake lock to reduce Android suspension
- Logs activity to `~/camera_captures/capture.log`
- Runs continuously under `termux-services`
