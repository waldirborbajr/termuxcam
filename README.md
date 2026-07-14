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
````

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

If you receive camera information in JSON format, the integration is working.

---

## 3. Determine the Front Camera ID

List available cameras:

```sh
termux-camera-info
```

Example:

```json
[
  {
    "id": "0",
    "facing": "back"
  },
  {
    "id": "1",
    "facing": "front"
  }
]
```

Note the camera ID associated with `"facing": "front"`.

If your front camera is not `"1"`, update the `cameraID` constant in `main.go`.

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
3. Send:

```text
/newbot
```

4. Follow the instructions.
5. Save the bot token.

Example:

```text
123456789:ABCDEFxxxxxxxxxxxxxxxxxxxxxxxx
```

---

## 6. Obtain Your Telegram Chat ID

Send any message to your bot first.

Open:

```text
https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates
```

Example:

```text
https://api.telegram.org/bot123456789:ABCDEFxxxxxxxxxxxxxxxxxxxxxxxx/getUpdates
```

You should see something similar to:

```json
{
  "ok": true,
  "result": [
    {
      "message": {
        "chat": {
          "id": 782816475
        }
      }
    }
  ]
}
```

Your Chat ID is:

```text
782816475
```

Save both values:

* `TG_BOT_TOKEN`
* `TG_CHAT_ID`

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

Expected:

```text
-rwxr-xr-x
```

> **Note:** This guide assumes all personal binaries are stored in `~/bins`. If you already maintain a different binaries directory, adjust the paths accordingly.

---

## 8. Test Manually

Before configuring a service, verify everything works.

```sh
export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

~/bins/termuxcam
```

Confirm:

* A photo is captured.
* The photo arrives in Telegram.
* No errors are displayed.

Stop with:

```text
Ctrl+C
```

---

## 9. Install termux-services

```sh
pkg install termux-services
```

Load the service environment:

```sh
source $PREFIX/etc/profile.d/start-services.sh
```

Verify:

```sh
which sv
```

Expected:

```text
/data/data/com.termux/files/usr/bin/sv
```

---

## 10. Install termuxcam as a Service

### 10.1 Create the Service Directory

```sh
mkdir -p ~/.termux/service/termuxcam
mkdir -p ~/.termux/service/termuxcam/log
```

Verify:

```sh
ls -la ~/.termux/service/termuxcam
```

---

### 10.2 Create the Run Script

```sh
cat <<'EOF' > ~/.termux/service/termuxcam/run
#!/data/data/com.termux/files/usr/bin/sh

export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

exec /data/data/com.termux/files/home/bins/termuxcam
EOF
```

Make it executable:

```sh
chmod +x ~/.termux/service/termuxcam/run
```

Verify:

```sh
ls -l ~/.termux/service/termuxcam/run
```

Expected:

```text
-rwxr-xr-x
```

---

### 10.3 Enable the Service

```sh
sv-enable termuxcam
```

---

### 10.4 Start the Service

```sh
sv up termuxcam
```

Check status:

```sh
sv status termuxcam
```

Expected:

```text
run: termuxcam: (pid XXXX) ...
```

---

### 10.5 View Logs

```sh
tail -f ~/.termux/var/service/termuxcam/log/main/current
```

If your Termux version stores logs elsewhere:

```sh
sv status termuxcam
```

and inspect the service directory.

---

## 11. Verify Automatic Startup

Close Termux completely.

Reopen it and run:

```sh
sv status termuxcam
```

The service should still be running.

You can also reboot the device and verify again.

---

## 12. Disable Battery Optimization (Recommended)

Many Android vendors aggressively terminate background processes.

Set:

**Settings → Apps → Termux → Battery → Unrestricted**

Manufacturer-specific settings may also need adjustment.

---

## 13. Shared Storage Access (Optional)

Allow access to Android shared storage:

```sh
termux-setup-storage
```

This is useful if you want to inspect captured photos before they are automatically removed.

---

## Service Management

| Action         | Command                                                    |
| -------------- | ---------------------------------------------------------- |
| Start          | `sv up termuxcam`                                          |
| Stop           | `sv down termuxcam`                                        |
| Restart        | `sv restart termuxcam`                                     |
| Status         | `sv status termuxcam`                                      |
| Enable on boot | `sv-enable termuxcam`                                      |
| Disable        | `sv-disable termuxcam`                                     |
| View logs      | `tail -f ~/.termux/var/service/termuxcam/log/main/current` |

---

## Troubleshooting

### "No such file or directory" when creating `run`

The service directory does not exist.

Create it first:

```sh
mkdir -p ~/.termux/service/termuxcam
```

### "permission denied"

Make the files executable:

```sh
chmod +x ~/bins/termuxcam
chmod +x ~/.termux/service/termuxcam/run
```

### No photos arrive in Telegram

Verify:

```sh
curl https://api.telegram.org/bot$TG_BOT_TOKEN/getMe
```

Verify your Chat ID:

```sh
curl https://api.telegram.org/bot$TG_BOT_TOKEN/getUpdates
```

### Camera capture fails

Verify:

```sh
termux-camera-info
```

and ensure the Termux:API application has Camera permission.

### Service cannot be started

Verify that the binary exists:

```sh
ls -l ~/bins/termuxcam
```

Verify that the run script points to the correct path:

```sh
cat ~/.termux/service/termuxcam/run
```

Verify that termux-services is loaded:

```sh
source $PREFIX/etc/profile.d/start-services.sh
```

---

## Technical Notes

* Captures an image every 5 minutes
* Uses the front camera by default
* Uploads via Telegram Bot API `sendPhoto`
* Deletes images only after successful upload
* Acquires a wake lock to reduce Android suspension
* Logs activity to `~/camera_captures/capture.log`
* Runs continuously under `termux-services`

