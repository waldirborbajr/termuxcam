package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	configFileName = "termuxcam.conf"
	captureTimeout = 45 * time.Second
	uploadTimeout  = 30 * time.Second

	// Guardrails: prevent a malformed/edited config from producing a
	// zero/negative ticker interval (which panics) or a hammering loop.
	minInterval = 1 * time.Minute
	maxInterval = 24 * time.Hour

	configFileMode = 0o600 // config may end up alongside logs; keep it user-only
	dirMode        = 0o700
	fileMode       = 0o600
)

// --- Physical hardware camera IDs ------------------------------------------
//
// These are what `termux-camera-photo -c <id>` actually expects, and they are
// a property of the DEVICE, not of this app's config. They are NOT guaranteed
// to be 0=front/1=back — on this phone, `termux-camera-info` reported:
//
//   {"id": "0", "facing": "back"}
//   {"id": "1", "facing": "front"}
//
// i.e. the opposite of the intuitive guess. If you ever run this on a
// different device, re-run `termux-camera-info` and update these two
// constants — everything else in the file only deals with the semantic
// "front"/"back" labels below and never touches a raw ID directly.
const (
	frontCameraHWID = "1"
	backCameraHWID  = "0"
)

var (
	exeDir      string
	outputDir   string
	logFilePath string
	tgBotToken  = strings.TrimSpace(os.Getenv("TG_BOT_TOKEN"))
	tgChatID    = strings.TrimSpace(os.Getenv("TG_CHAT_ID"))
	interval    = 5 * time.Minute // fallback, used only if config is missing/invalid
	cameraMode  = 1               // 0=back, 1=front, 2=both (config-facing semantic value)

	httpClient = &http.Client{
		Timeout: uploadTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
)

// camSpec pairs a human-readable label with the physical hardware ID needed
// to actually address that camera. Everything downstream of camerasForMode
// works off camSpec, so there is exactly one place (the constants above)
// where a device's physical camera numbering can be wrong.
type camSpec struct {
	label string // used in filenames, captions, and logs — always correct by construction
	hwID  string // passed verbatim to `termux-camera-photo -c`
}

// camerasForMode translates the config's semantic camera selection into the
// concrete camera(s) to capture from, in a fixed, predictable order.
// Config convention: 0=back, 1=front, 2=both.
func camerasForMode(mode int) []camSpec {
	switch mode {
	case 0:
		return []camSpec{{"back", backCameraHWID}}
	case 1:
		return []camSpec{{"front", frontCameraHWID}}
	case 2:
		return []camSpec{{"front", frontCameraHWID}, {"back", backCameraHWID}}
	default:
		return []camSpec{{"front", frontCameraHWID}}
	}
}

func getExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), "bins")
	}
	return filepath.Dir(exe)
}

func logMsg(msg string) {
	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] %s", ts, msg)
	fmt.Println(line)

	if err := os.MkdirAll(outputDir, dirMode); err != nil {
		fmt.Printf("[%s] WARN: could not create output dir: %v\n", ts, err)
		return
	}
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		fmt.Printf("[%s] WARN: could not open log file: %v\n", ts, err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		fmt.Printf("[%s] WARN: could not write log file: %v\n", ts, err)
	}
}

// stripInlineComment removes a trailing "# ..." comment from a config value.
// Without this, "capture=3h  # or 2h, 30m" fails to parse as a duration and
// silently falls back to the default interval.
func stripInlineComment(s string) string {
	if idx := strings.Index(s, "#"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func loadConfig() {
	configPath := filepath.Join(exeDir, configFileName)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultCfg := `# termuxcam configuration
# capture=15m   (m = minutes, h = hours)
capture=5m

# camera=0  -> Back only
# camera=1  -> Front only (default)
# camera=2  -> Both cameras
camera=1
`
		if err := os.WriteFile(configPath, []byte(defaultCfg), configFileMode); err != nil {
			logMsg("ERROR: failed to create default config: " + err.Error())
		} else {
			logMsg("Created default config: " + configPath)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		logMsg(fmt.Sprintf("ERROR: failed to read config (%v), using defaults: interval=%s cameraMode=%d", err, interval, cameraMode))
		return
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = stripInlineComment(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			logMsg(fmt.Sprintf("WARN: ignoring malformed config line: %q", raw))
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "capture":
			d, err := parseDuration(val)
			if err != nil {
				logMsg(fmt.Sprintf("WARN: invalid capture value %q (%v), keeping %s", val, err, interval))
				continue
			}
			if d < minInterval || d > maxInterval {
				logMsg(fmt.Sprintf("WARN: capture value %s out of allowed range [%s, %s], keeping %s", d, minInterval, maxInterval, interval))
				continue
			}
			interval = d
		case "camera":
			c, err := strconv.Atoi(val)
			if err != nil || c < 0 || c > 2 {
				logMsg(fmt.Sprintf("WARN: invalid camera value %q, keeping %d", val, cameraMode))
				continue
			}
			cameraMode = c
		default:
			logMsg(fmt.Sprintf("WARN: unknown config key %q, ignoring", key))
		}
	}

	labels := make([]string, 0, 2)
	for _, c := range camerasForMode(cameraMode) {
		labels = append(labels, fmt.Sprintf("%s(hw=%s)", c.label, c.hwID))
	}
	logMsg(fmt.Sprintf("Config loaded -> interval=%s | cameraMode=%d -> %s | config=%s",
		interval, cameraMode, strings.Join(labels, ", "), configPath))
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(s, "h"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "h"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid hour value %q", s)
		}
		return time.Duration(n) * time.Hour, nil
	case strings.HasSuffix(s, "m"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "m"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid minute value %q", s)
		}
		return time.Duration(n) * time.Minute, nil
	default:
		return 0, fmt.Errorf("duration %q missing 'h' or 'm' suffix", s)
	}
}

func acquireWakeLock() {
	if _, err := exec.LookPath("termux-wake-lock"); err != nil {
		logMsg("WARN: termux-wake-lock not found, skipping (device may sleep)")
		return
	}
	if err := exec.Command("termux-wake-lock").Run(); err != nil {
		logMsg("WARN: termux-wake-lock failed: " + err.Error())
	}
}

func releaseWakeLock() {
	if err := exec.Command("termux-wake-unlock").Run(); err != nil {
		logMsg("WARN: termux-wake-unlock failed: " + err.Error())
	}
}

// capturePhoto takes a photo with the given physical hardware camera ID.
// label is used only for the output filename — it must already be the
// correct semantic label (from camSpec), not derived from hwID here.
func capturePhoto(ctx context.Context, hwID, label string) (string, error) {
	if _, err := exec.LookPath("termux-camera-photo"); err != nil {
		return "", fmt.Errorf("termux-camera-photo not found: %w", err)
	}

	ts := time.Now().Format("20060102_150405")
	outfile := filepath.Join(outputDir, fmt.Sprintf("capture_%s_%s.jpg", label, ts))

	cctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "termux-camera-photo", "-c", hwID, outfile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("Capture failed (%s, hw=%s): %s", label, hwID, stderr.String()))
		os.Remove(outfile)
		return "", err
	}

	info, err := os.Stat(outfile)
	if err != nil || info.Size() == 0 {
		os.Remove(outfile)
		return "", fmt.Errorf("empty or missing capture file")
	}

	logMsg(fmt.Sprintf("Captured successfully (%s, hw=%s): %s", label, hwID, outfile))
	return outfile, nil
}

func sendToTelegram(ctx context.Context, photoPath, caption string) bool {
	// Defense in depth: never let a crafted photoPath escape outputDir.
	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		logMsg("ERROR: cannot resolve output dir: " + err.Error())
		return false
	}
	absPhoto, err := filepath.Abs(photoPath)
	if err != nil || !strings.HasPrefix(absPhoto, absOut+string(os.PathSeparator)) {
		logMsg("ERROR: refusing to upload file outside output dir: " + photoPath)
		return false
	}

	file, err := os.Open(photoPath)
	if err != nil {
		logMsg("ERROR: cannot open photo for upload: " + err.Error())
		return false
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writer.WriteField("chat_id", tgChatID); err != nil {
		logMsg("ERROR: writing chat_id field: " + err.Error())
		return false
	}
	if err := writer.WriteField("caption", caption); err != nil {
		logMsg("ERROR: writing caption field: " + err.Error())
		return false
	}
	part, err := writer.CreateFormFile("photo", filepath.Base(photoPath))
	if err != nil {
		logMsg("ERROR: creating form file: " + err.Error())
		return false
	}
	if _, err := io.Copy(part, file); err != nil {
		logMsg("ERROR: copying photo into request: " + err.Error())
		return false
	}
	if err := writer.Close(); err != nil {
		logMsg("ERROR: closing multipart writer: " + err.Error())
		return false
	}

	url := "https://api.telegram.org/bot" + tgBotToken + "/sendPhoto"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		logMsg("ERROR: building request: " + err.Error())
		return false
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		logMsg("ERROR: telegram request failed: " + err.Error())
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logMsg(fmt.Sprintf("ERROR: telegram returned HTTP %d", resp.StatusCode))
		return false
	}

	var r struct {
		Ok          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		logMsg("ERROR: decoding telegram response: " + err.Error())
		return false
	}
	if !r.Ok {
		logMsg("ERROR: telegram rejected upload: " + r.Description)
	}
	return r.Ok
}

func runOnce(ctx context.Context) {
	now := time.Now().Format("2006-01-02 15:04:05")

	for _, cam := range camerasForMode(cameraMode) {
		photo, err := capturePhoto(ctx, cam.hwID, cam.label)
		if err != nil {
			continue // one camera failing shouldn't block the other in "both" mode
		}

		caption := fmt.Sprintf("%s camera: %s", strings.Title(cam.label), now)

		if sendToTelegram(ctx, photo, caption) {
			if err := os.Remove(photo); err != nil {
				logMsg("WARN: failed to delete local file after upload: " + err.Error())
			} else {
				logMsg("File sent and deleted")
			}
		} else {
			logMsg("Upload failed - keeping local file")
		}
	}
}

func main() {
	if tgBotToken == "" || tgChatID == "" {
		fmt.Println("TG_BOT_TOKEN or TG_CHAT_ID not set")
		os.Exit(1)
	}

	exeDir = getExeDir()
	outputDir = filepath.Join(exeDir, "camera_captures")
	logFilePath = filepath.Join(outputDir, "capture.log")

	if err := os.MkdirAll(outputDir, dirMode); err != nil {
		fmt.Println("FATAL: cannot create output dir:", err)
		os.Exit(1)
	}

	loadConfig()

	acquireWakeLock()
	defer releaseWakeLock()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam started | Binary dir: %s | Interval: %s", exeDir, interval))

	runOnce(ctx) // immediate first capture

	for {
		select {
		case <-ticker.C:
			runOnce(ctx)
		case <-ctx.Done():
			logMsg("Shutting down on signal")
			return
		}
	}
}
