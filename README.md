<p align="center">
  <img width="256" height="256" src="./assets/logo.png" />
</p>
<h1 align="center">TMS, A clean, fast, and modern tmux session manager written in Go</h1>

# termuxcam

Periodic front-camera capture on Android, running natively in Termux (no root required), with automatic upload to Telegram and cleanup of the local file once the upload is confirmed.

Written in Go — a single compiled binary, no external runtime dependency (Python, Node, etc.), designed to run as a long-lived service via `termux-services`, surviving Termux restarts and Android's doze mode.

**Flow:** capture via `termux-camera-photo` (Termux:API) → send the image to a Telegram bot/chat → delete the local file only after the upload is confirmed successful.

---

## Prerequisites

- An Android device with the **Termux** app installed (F-Droid build recommended — the Play Store version is discontinued)
- The **Termux:API** app installed (F-Droid) — this is the bridge between Termux and Android hardware (camera, sensors, etc.)
- A Telegram bot already created (see step 5)

---

## 1. Install Go

```sh
pkg update
pkg install golang
```

Verify the installation:

```sh
go version
```

---

## 2. Install Termux:API

The `termux-camera-photo` binary used to access the camera only exists after installing the `termux-api` package **and** having the separate **Termux:API** app installed (they work together — the package is the CLI interface, the app is what holds the Android camera permission).

```sh
pkg install termux-api
```

Then:
1. Install the **Termux:API** app from F-Droid (same store you installed Termux from)
2. Open the app once
3. Go to **Android Settings → Apps → Termux:API → Permissions** and grant **Camera** access

---

## 3. Confirm the front camera index

Each device numbers its cameras differently. Run:

```sh
termux-camera-info
```

This returns a JSON listing available camera `id`s and which `facing` (`front`/`back`) each one is. Note the `id` for the front camera — you'll need this value (constant `cameraID` in `main.go`, default `"1"`).

---

## 4. Get the source code

```sh
git clone https://github.com/waldirborbajr/termuxcam.git
cd termuxcam
```

If your device's front camera index isn't `1`, edit the `cameraID` constant in `main.go` before building.

---

## 5. Create the Telegram bot and get your credentials

1. On Telegram, message **@BotFather**
2. Send `/newbot` and follow the prompts → you'll receive a **token** (format `123456:ABC-...`)
3. Send any message to your newly created bot (required, otherwise the next step returns nothing)
4. In a browser, visit (replacing with your token):
   ```
   https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates
   ```
5. In the returned JSON, find `"chat":{"id": ...}` — that number is your `chat_id`

Keep both values handy: `TG_BOT_TOKEN` and `TG_CHAT_ID`. You'll need them in steps 7 and 8.

---

## 6. Build

From inside the project folder:

```sh
go mod init termuxcam
go build -o termuxcapture main.go context.go
```

> If the repository already ships with a `go.mod` file, skip `go mod init` — it already exists.

This produces the `termuxcapture` binary in the current folder.

---

## 7. Test manually (optional, but recommended before turning it into a service)

```sh
export TG_BOT_TOKEN="123456:ABC-your-token"
export TG_CHAT_ID="your_chat_id"
./termuxcapture
```

Check that a photo arrives in your Telegram chat. Press `Ctrl+C` to stop — the program releases its wake-lock automatically on exit.

If capture fails here, fix it before setting up the service (steps 8+); running as a service only hides errors in a log file, which makes initial troubleshooting harder.

---

## 8. Install as a persistent service (`termux-services`)

This keeps the program running in the background, surviving Termux being closed and device restarts, using the `runit` supervisor.

### 8.1 Install the service manager

```sh
pkg install termux-services
```

Close and reopen Termux after this step (it needs to restart the `init` process).

### 8.2 Copy the binary to where the service expects it

```sh
cp termuxcapture ~/termuxcapture
```

### 8.3 Create the service directory

```sh
mkdir -p ~/.termux/service/termuxcapture/log
```

### 8.4 Create the `run` script (what the supervisor executes)

```sh
cat <<'EOF' > ~/.termux/service/termuxcapture/run
#!/data/data/com.termux/files/usr/bin/sh
export TG_BOT_TOKEN="123456:ABC-your-token"
export TG_CHAT_ID="your_chat_id"
exec /data/data/com.termux/files/home/termuxcapture
EOF
chmod +x ~/.termux/service/termuxcapture/run
```

> Replace the token and chat_id with the values obtained in step 5.

### 8.5 Enable and start the service

```sh
sv-enable termuxcapture
sv up termuxcapture
```

### 8.6 Check status and follow logs

```sh
sv status termuxcapture
tail -f ~/.termux/var/service/termuxcapture/log/main/current
```

---

## 9. (Optional) Disable battery optimization for Termux

To reduce the chance of Android suspending the background process:

**Android Settings → Apps → Termux → Battery → Unrestricted** (exact path varies by manufacturer).

## 10. (Optional) Access photos outside Termux's sandbox

```sh
termux-setup-storage
```

Grants access to Android's shared storage, letting you browse the `camera_captures` folder with a regular file manager — useful for inspecting images before they're sent/deleted automatically.

---

## Useful maintenance commands

| Action | Command |
|---|---|
| Stop the service | `sv down termuxcapture` |
| Restart the service | `sv restart termuxcapture` |
| Disable the service | `sv-disable termuxcapture` |
| Follow logs in real time | `tail -f ~/.termux/var/service/termuxcapture/log/main/current` |

---

## How it works (technical summary)

- Every 5 minutes, the program calls `termux-camera-photo` to capture an image from the front camera
- The image is sent via HTTP multipart to the Telegram API (`sendPhoto`)
- On successful delivery, the local file is deleted; on failure, it's kept for a manual retry
- A `termux-wake-lock` is acquired at startup and released on exit, preventing Android from suspending the process mid-cycle
- All events are logged to `~/camera_captures/capture.log`
