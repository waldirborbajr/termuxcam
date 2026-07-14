
<p align="center">
<img width="256" height="256" src="./assets/logo.png" />
</p>
<h1 align="center">

  termuxcam - Periodic Android Camera Capture for Termux
</h1>


Periodic front-camera capture on Android, running natively in Termux (no
root required), with automatic upload to Telegram and cleanup of the
local file once the upload is confirmed.

Written in Go --- a single compiled binary with no external runtime
dependency (Python, Node.js, etc.), designed to run as a long-lived
service via `termux-services`.

**Flow:** capture via `termux-camera-photo` (Termux:API) → upload to
Telegram → delete local image after successful upload.

------------------------------------------------------------------------

# Prerequisites

-   Android device
-   Termux (F-Droid version recommended)
-   Termux:API Android application installed
-   `termux-api` package installed
-   Internet connection
-   Telegram account

------------------------------------------------------------------------

# 1. Install Go

``` sh
pkg update && pkg upgrade -y
pkg install golang git
```

Verify:

``` sh
go version
```

------------------------------------------------------------------------

# 2. Install Termux:API Dependencies

`termuxcam` requires the `termux-camera-photo` command provided by the
`termux-api` package.

Install:

``` sh
pkg install termux-api
```

Install the **Termux:API Android application** from F-Droid.

Both components are required:

  -----------------------------------------------------------------------
  Component                           Purpose
  ----------------------------------- -----------------------------------
  `termux-api` package                Provides commands like
                                      `termux-camera-photo`

  Termux:API Android app              Provides Android hardware access
                                      and permissions
  -----------------------------------------------------------------------

Open the Termux:API application once and grant Camera permission.

Verify:

``` sh
which termux-camera-photo
```

Expected:

``` text
/data/data/com.termux/files/usr/bin/termux-camera-photo
```

Test:

``` sh
termux-camera-info
```

------------------------------------------------------------------------

# 3. Determine Front Camera ID

``` sh
termux-camera-info
```

Example:

``` json
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

Update the `cameraID` constant in `main.go` if your front camera ID is
different.

------------------------------------------------------------------------

# 4. Clone Repository

``` sh
git clone https://github.com/waldirborbajr/termuxcam.git
cd termuxcam
```

------------------------------------------------------------------------

# 5. Create Telegram Bot

1.  Open Telegram.
2.  Contact **@BotFather**.
3.  Send:

``` text
/newbot
```

4.  Follow instructions.
5.  Save the generated bot token.

Example:

``` text
123456789:ABCDEFxxxxxxxxxxxxxxxx
```

------------------------------------------------------------------------

# 6. Get Telegram Chat ID

Send a message to your bot first.

Open:

``` text
https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates
```

Find:

``` json
"chat": {
  "id": 782816475
}
```

Save:

-   `TG_BOT_TOKEN`
-   `TG_CHAT_ID`

------------------------------------------------------------------------

# 7. Build

Create a binary directory:

``` sh
mkdir -p ~/bins
```

Build:

``` sh
go build -o ~/bins/termuxcam main.go context.go
```

Verify:

``` sh
ls -lh ~/bins/termuxcam
```

------------------------------------------------------------------------

# 8. Test Manually

``` sh
export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

~/bins/termuxcam
```

Confirm the photo arrives in Telegram.

------------------------------------------------------------------------

# 9. Install termux-services

``` sh
pkg install termux-services
```

Load environment:

``` sh
source $PREFIX/etc/profile.d/start-services.sh
```

Verify:

``` sh
which sv
```

------------------------------------------------------------------------

# 10. Install termuxcam Service

## 10.1 Create Service Directory

``` sh
mkdir -p ~/.termux/service/termuxcam
mkdir -p ~/.termux/service/termuxcam/log
```

------------------------------------------------------------------------

## 10.2 Create Run Script

``` sh
cat <<'EOF' > ~/.termux/service/termuxcam/run
#!/data/data/com.termux/files/usr/bin/sh

export PATH=/data/data/com.termux/files/usr/bin:$PATH

export TG_BOT_TOKEN="YOUR_TOKEN"
export TG_CHAT_ID="YOUR_CHAT_ID"

exec /data/data/com.termux/files/home/bins/termuxcam
EOF
```

Make executable:

``` sh
chmod +x ~/.termux/service/termuxcam/run
```

------------------------------------------------------------------------

## 10.3 Enable Service

Create the symbolic link used by `runit`:

``` sh
ln -s ~/.termux/service/termuxcam $PREFIX/var/service/termuxcam
```

Verify:

``` sh
ls -la $PREFIX/var/service
```

Expected:

``` text
termuxcam -> /data/data/com.termux/files/home/.termux/service/termuxcam
```

------------------------------------------------------------------------

## 10.4 Start Service

``` sh
sv up termuxcam
```

Check:

``` sh
sv status termuxcam
```

------------------------------------------------------------------------

## 10.5 Monitor Logs

Application log:

``` sh
tail -f ~/camera_captures/capture.log
```

Service output:

``` sh
tail -f ~/.termux/var/service/termuxcam/log/main/current
```

------------------------------------------------------------------------

# Troubleshooting

## termux-camera-photo executable not found

Error:

``` text
exec: "termux-camera-photo": executable file not found in $PATH
```

Install:

``` sh
pkg install termux-api
```

Verify:

``` sh
which termux-camera-photo
```

------------------------------------------------------------------------

## TG_BOT_TOKEN / TG_CHAT_ID not set

Check:

``` sh
cat ~/.termux/service/termuxcam/run
```

Restart:

``` sh
sv restart termuxcam
```

------------------------------------------------------------------------

## Service directory not found

Create the symbolic link:

``` sh
ln -s ~/.termux/service/termuxcam $PREFIX/var/service/termuxcam
```

Start:

``` sh
sv up termuxcam
```

------------------------------------------------------------------------

# Service Management

  Action    Command
  --------- ------------------------
  Start     `sv up termuxcam`
  Stop      `sv down termuxcam`
  Restart   `sv restart termuxcam`
  Status    `sv status termuxcam`

------------------------------------------------------------------------

# Battery Optimization

Disable Android battery restrictions:

**Settings → Apps → Termux → Battery → Unrestricted**

------------------------------------------------------------------------

# Technical Notes

-   Captures every 5 minutes
-   Uses front camera
-   Uploads using Telegram Bot API
-   Deletes files only after successful upload
-   Uses Termux wake lock
-   Logs to `~/camera_captures/capture.log`
-   Runs continuously using `termux-services`
