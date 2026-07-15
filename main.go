package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"io"
	"math/bits"
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
	stateFileName  = "motion_state.json"
	captureTimeout = 45 * time.Second
	uploadTimeout  = 30 * time.Second

	// Guardrails: prevent a malformed/edited config from producing a
	// zero/negative ticker interval (which panics) or a hammering loop.
	minInterval = 1 * time.Minute
	maxInterval = 24 * time.Hour

	// dHash grid: 9 columns x 8 rows yields 8 horizontal comparisons per row,
	// 64 bits total — a standard, well-tested difference-hash size.
	hashGridW = 9
	hashGridH = 8
	hashBits  = 64 // (hashGridW-1) * hashGridH

	defaultMotionEnabled   = true
	defaultMotionThreshold = 6 // bits out of 64 that must differ to count as "changed" (~9%)
	defaultHeartbeat       = 0 // disabled by default

	configFileMode = 0o600
	dirMode        = 0o700
	fileMode       = 0o600
)

// --- Physical hardware camera IDs ------------------------------------------
//
// These are what `termux-camera-photo -c <id>` actually expects, and they are
// a property of the DEVICE, not of this app's config. Confirm with
// `termux-camera-info` and update if different on your phone. Nothing else
// in this file ever touches a raw ID directly — everything downstream works
// off the semantic "front"/"back" labels.
const (
	frontCameraHWID = "1"
	backCameraHWID  = "0"
)

var (
	exeDir      string
	outputDir   string
	logFilePath string
	statePath   string
	tgBotToken  = strings.TrimSpace(os.Getenv("TG_BOT_TOKEN"))
	tgChatID    = strings.TrimSpace(os.Getenv("TG_CHAT_ID"))

	interval    = 5 * time.Minute // fallback, used only if config is missing/invalid
	cameraMode  = 1               // 0=back, 1=front, 2=both (config-facing semantic value)

	motionEnabled   = defaultMotionEnabled
	motionThreshold = defaultMotionThreshold
	heartbeat       = defaultHeartbeat // 0 = disabled

	httpClient = &http.Client{
		Timeout: uploadTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
)

// camSpec pairs a human-readable label with the physical hardware ID needed
// to actually address that camera.
type camSpec struct {
	label string
	hwID  string
}

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

// --- Config -----------------------------------------------------------

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

# Capture interval — a number followed by 'm' (minutes) or 'h' (hours)
capture=5m

# Camera mode:
#   0 = back only
#   1 = front only (default)
#   2 = both (captures and uploads one photo from each camera per cycle)
camera=1

# Motion detection: skip uploading (and delete) a capture that looks
# essentially identical to the previous one from the same camera.
# Compares a small perceptual hash of each frame; costs nothing beyond
# a few milliseconds of CPU per capture, no network/API calls involved.
motion=true

# How different two frames must be to count as "changed", out of 64.
# Lower = more sensitive (more uploads, more false positives from noise/
# light flicker). Higher = less sensitive (may miss subtle changes).
# 4-10 is a reasonable range for a static indoor camera.
motion_threshold=6

# Force an upload at least this often even with no detected motion, so
# you have periodic confirmation the service is still alive. 0 disables
# this (only motion-triggered uploads). Same duration format as capture.
heartbeat=1h
`
		if err := os.WriteFile(configPath, []byte(defaultCfg), configFileMode); err != nil {
			logMsg("ERROR: failed to create default config: " + err.Error())
		} else {
			logMsg("Created default config: " + configPath)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		logMsg(fmt.Sprintf("ERROR: failed to read config (%v), using defaults", err))
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
		case "motion":
			b, err := strconv.ParseBool(val)
			if err != nil {
				logMsg(fmt.Sprintf("WARN: invalid motion value %q, keeping %v", val, motionEnabled))
				continue
			}
			motionEnabled = b
		case "motion_threshold":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 || n > hashBits {
				logMsg(fmt.Sprintf("WARN: invalid motion_threshold value %q, keeping %d", val, motionThreshold))
				continue
			}
			motionThreshold = n
		case "heartbeat":
			if val == "0" {
				heartbeat = 0
				continue
			}
			d, err := parseDuration(val)
			if err != nil {
				logMsg(fmt.Sprintf("WARN: invalid heartbeat value %q (%v), keeping %s", val, err, heartbeat))
				continue
			}
			heartbeat = d
		default:
			logMsg(fmt.Sprintf("WARN: unknown config key %q, ignoring", key))
		}
	}

	labels := make([]string, 0, 2)
	for _, c := range camerasForMode(cameraMode) {
		labels = append(labels, fmt.Sprintf("%s(hw=%s)", c.label, c.hwID))
	}
	logMsg(fmt.Sprintf(
		"Config loaded -> interval=%s | cameraMode=%d -> %s | motion=%v threshold=%d/%d | heartbeat=%s | config=%s",
		interval, cameraMode, strings.Join(labels, ", "), motionEnabled, motionThreshold, hashBits, heartbeat, configPath,
	))
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

// --- Motion detection state (persisted across restarts) -------------------

type camState struct {
	Hash     uint64    `json:"hash"`
	LastSent time.Time `json:"last_sent"`
}

type motionState struct {
	Cameras map[string]camState `json:"cameras"`
}

func loadState() motionState {
	st := motionState{Cameras: make(map[string]camState)}
	data, err := os.ReadFile(statePath)
	if err != nil {
		return st // no prior state (first run, or file missing) — that's fine
	}
	if err := json.Unmarshal(data, &st); err != nil {
		logMsg(fmt.Sprintf("WARN: could not parse motion state (%v), starting fresh", err))
		return motionState{Cameras: make(map[string]camState)}
	}
	if st.Cameras == nil {
		st.Cameras = make(map[string]camState)
	}
	return st
}

func saveState(st motionState) {
	data, err := json.Marshal(st)
	if err != nil {
		logMsg("WARN: could not marshal motion state: " + err.Error())
		return
	}
	if err := os.WriteFile(statePath, data, fileMode); err != nil {
		logMsg("WARN: could not write motion state: " + err.Error())
	}
}

// computeDHash produces a 64-bit difference hash of the image at path.
// It downsamples to a hashGridW x hashGridH grayscale grid using nearest-
// neighbor sampling (no external imaging libraries needed) and encodes
// whether each pixel is brighter than its right neighbor.
func computeDHash(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return 0, fmt.Errorf("zero-sized image")
	}

	var grid [hashGridH][hashGridW]uint32
	for gy := 0; gy < hashGridH; gy++ {
		srcY := bounds.Min.Y + gy*h/hashGridH
		for gx := 0; gx < hashGridW; gx++ {
			srcX := bounds.Min.X + gx*w/hashGridW
			gray := color.GrayModel.Convert(img.At(srcX, srcY)).(color.Gray)
			grid[gy][gx] = uint32(gray.Y)
		}
	}

	var hash uint64
	bit := 0
	for gy := 0; gy < hashGridH; gy++ {
		for gx := 0; gx < hashGridW-1; gx++ {
			if grid[gy][gx] > grid[gy][gx+1] {
				hash |= 1 << uint(bit)
			}
			bit++
		}
	}
	return hash, nil
}

func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// --- Wake lock --------------------------------------------------------

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

// --- Capture ------------------------------------------------------------

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

// --- Telegram -------------------------------------------------------------

func sendToTelegram(ctx context.Context, photoPath, caption string) bool {
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

// --- Orchestration ----------------------------------------------------

func runOnce(ctx context.Context, state motionState) motionState {
	now := time.Now()
	nowStr := now.Format("2006-01-02 15:04:05")

	for _, cam := range camerasForMode(cameraMode) {
		photo, err := capturePhoto(ctx, cam.hwID, cam.label)
		if err != nil {
			continue // one camera failing shouldn't block the other in "both" mode
		}

		shouldSend := true
		reason := ""
		var newHash uint64
		prev, hadPrev := state.Cameras[cam.label]

		if motionEnabled {
			h, herr := computeDHash(photo)
			if herr != nil {
				logMsg(fmt.Sprintf("WARN: could not hash %s (%v), sending anyway", photo, herr))
			} else {
				newHash = h
				if hadPrev {
					dist := hammingDistance(h, prev.Hash)
					heartbeatDue := heartbeat > 0 && !prev.LastSent.IsZero() && now.Sub(prev.LastSent) >= heartbeat
					if dist < motionThreshold && !heartbeatDue {
						shouldSend = false
						reason = fmt.Sprintf("no significant change (distance=%d/%d, threshold=%d)", dist, hashBits, motionThreshold)
					} else if heartbeatDue {
						reason = fmt.Sprintf("heartbeat due (distance=%d/%d)", dist, hashBits)
					} else {
						reason = fmt.Sprintf("motion detected (distance=%d/%d)", dist, hashBits)
					}
				} else {
					reason = "first capture for this camera, no baseline yet"
				}
				// Always track the latest hash for the next comparison,
				// regardless of whether this frame was sent — otherwise a
				// slow scene drift would never register as "changed".
				state.Cameras[cam.label] = camState{Hash: newHash, LastSent: prev.LastSent}
			}
		}

		if !shouldSend {
			logMsg(fmt.Sprintf("Skipped upload (%s): %s", cam.label, reason))
			os.Remove(photo)
			continue
		}

		if reason != "" {
			logMsg(fmt.Sprintf("Sending (%s): %s", cam.label, reason))
		}

		caption := fmt.Sprintf("%s camera: %s", strings.Title(cam.label), nowStr)

		if sendToTelegram(ctx, photo, caption) {
			if err := os.Remove(photo); err != nil {
				logMsg("WARN: failed to delete local file after upload: " + err.Error())
			} else {
				logMsg("File sent and deleted")
			}
			if motionEnabled {
				cs := state.Cameras[cam.label]
				cs.LastSent = now
				state.Cameras[cam.label] = cs
			}
		} else {
			logMsg("Upload failed - keeping local file")
		}
	}

	return state
}

func main() {
	if tgBotToken == "" || tgChatID == "" {
		fmt.Println("TG_BOT_TOKEN or TG_CHAT_ID not set")
		os.Exit(1)
	}

	exeDir = getExeDir()
	outputDir = filepath.Join(exeDir, "camera_captures")
	logFilePath = filepath.Join(outputDir, "capture.log")
	statePath = filepath.Join(outputDir, stateFileName)

	if err := os.MkdirAll(outputDir, dirMode); err != nil {
		fmt.Println("FATAL: cannot create output dir:", err)
		os.Exit(1)
	}

	loadConfig()
	state := loadState()

	acquireWakeLock()
	defer releaseWakeLock()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam started | Binary dir: %s | Interval: %s", exeDir, interval))

	state = runOnce(ctx, state) // immediate first capture
	saveState(state)

	for {
		select {
		case <-ticker.C:
			state = runOnce(ctx, state)
			saveState(state)
		case <-ctx.Done():
			logMsg("Shutting down on signal")
			return
		}
	}
}
