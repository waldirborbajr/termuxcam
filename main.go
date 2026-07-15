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
	"sync"
	"syscall"
	"time"
)

const (
	configFileName = "termuxcam.conf"
	stateFileName  = "motion_state.json"
	captureTimeout = 45 * time.Second
	uploadTimeout  = 30 * time.Second

	// Guardrails
	minInterval = 1 * time.Minute
	maxInterval = 24 * time.Hour

	// dHash settings
	hashGridW = 9
	hashGridH = 8
	hashBits  = 64

	defaultMotionEnabled    = true
	defaultMotionThreshold  = 6
	defaultHeartbeat        = 0

	configFileMode = 0o600
	dirMode        = 0o700
	fileMode       = 0o600
)

// Camera hardware IDs
const (
	frontCameraHWID = "1"
	backCameraHWID  = "0"
)

var (
	exeDir      string
	outputDir   string
	logFilePath string
	statePath   string

	tgBotToken = strings.TrimSpace(os.Getenv("TG_BOT_TOKEN"))
	tgChatID   = strings.TrimSpace(os.Getenv("TG_CHAT_ID"))

	interval        = 5 * time.Minute
	cameraMode      = 1 // 0=back, 1=front, 2=both
	motionEnabled   = defaultMotionEnabled
	motionThreshold = defaultMotionThreshold
	heartbeat       time.Duration

	httpClient = &http.Client{
		Timeout: uploadTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	// Metrics for /status
	startTime             = time.Now()
	lastSuccessfulCapture time.Time
	metricsMutex          sync.RWMutex
)

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

// Get last log line for status
func getLastLogLine() string {
	data, err := os.ReadFile(logFilePath)
	if err != nil || len(data) == 0 {
		return "No logs yet"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return lines[len(lines)-1]
}

// --- System Information for /status ---
func getDiskUsage() string {
	out, _ := exec.Command("df", "-h", outputDir).Output()
	lines := strings.Split(string(out), "\n")
	if len(lines) > 1 {
		f := strings.Fields(lines[1])
		if len(f) >= 4 {
			return fmt.Sprintf("%s / %s", f[2], f[1])
		}
	}
	return "Unknown"
}

func getMemoryUsage() string {
	out, _ := exec.Command("free", "-h").Output()
	lines := strings.Split(string(out), "\n")
	if len(lines) > 1 {
		f := strings.Fields(lines[1])
		if len(f) >= 3 {
			return fmt.Sprintf("%s / %s", f[2], f[1])
		}
	}
	return "Unknown"
}

func getCPUUsagePercent() string {
	out, _ := exec.Command("top", "-bn1").Output()
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Cpu(s)") {
			parts := strings.Split(line, ",")
			if len(parts) > 0 {
				idleStr := strings.TrimSpace(strings.Split(parts[len(parts)-1], "%")[0])
				if idle, err := strconv.ParseFloat(idleStr, 64); err == nil {
					return fmt.Sprintf("%.1f%%", 100-idle)
				}
			}
		}
	}
	return "N/A"
}

func getCPUTemperature() string {
	out, err := exec.Command("cat", "/sys/class/thermal/thermal_zone0/temp").Output()
	if err == nil {
		if temp, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			return fmt.Sprintf("%.1f°C", float64(temp)/1000)
		}
	}
	return "N/A"
}

func getLocalIP() string {
	out, _ := exec.Command("ip", "route", "get", "1").Output()
	parts := strings.Fields(string(out))
	for i, p := range parts {
		if p == "src" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "N/A"
}

func getPublicIP() string {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "N/A"
	}
	defer resp.Body.Close()
	ip, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(ip))
}

func getBatteryInfo() string {
	out, err := exec.Command("termux-battery-status").Output()
	if err != nil {
		return "N/A"
	}
	var bat struct {
		Percentage  int     `json:"percentage"`
		Temperature float64 `json:"temperature"`
		Health      string  `json:"health"`
	}
	json.Unmarshal(out, &bat)
	return fmt.Sprintf("%d%% | %.1f°C | %s", bat.Percentage, bat.Temperature, bat.Health)
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
# Camera mode: 0=back only, 1=front only, 2=both
camera=1
# Motion detection
motion=true
motion_threshold=6
# Force an upload at least this often even with no detected motion (0 = disabled)
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
				logMsg(fmt.Sprintf("WARN: capture value %s out of allowed range, keeping %s", d, interval))
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
			if val == "0" || val == "0h" || val == "0m" {
				heartbeat = 0
				continue
			}
			d, err := parseDuration(val)
			if err != nil {
				logMsg(fmt.Sprintf("WARN: invalid heartbeat value %q (%v), keeping %s", val, err, heartbeat))
				continue
			}
			heartbeat = d
		}
	}

	labels := make([]string, 0, 2)
	for _, c := range camerasForMode(cameraMode) {
		labels = append(labels, fmt.Sprintf("%s(hw=%s)", c.label, c.hwID))
	}

	logMsg(fmt.Sprintf(
		"Config loaded -> interval=%s | cameraMode=%d -> %s | motion=%v threshold=%d/%d | heartbeat=%s",
		interval, cameraMode, strings.Join(labels, ", "), motionEnabled, motionThreshold, hashBits, heartbeat,
	))
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(s, "h"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "h"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid hour value")
		}
		return time.Duration(n) * time.Hour, nil
	case strings.HasSuffix(s, "m"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "m"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid minute value")
		}
		return time.Duration(n) * time.Minute, nil
	default:
		return 0, fmt.Errorf("duration missing 'h' or 'm' suffix")
	}
}

// --- Motion detection state -------------------------------------------
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
		return st
	}
	if err := json.Unmarshal(data, &st); err != nil {
		logMsg("WARN: could not parse motion state, starting fresh")
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
	os.WriteFile(statePath, data, fileMode)
}

// --- dHash ------------------------------------------------------------
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
		logMsg("WARN: termux-wake-lock not found, skipping")
		return
	}
	exec.Command("termux-wake-lock").Run()
}

func releaseWakeLock() {
	exec.Command("termux-wake-unlock").Run()
}

// --- Telegram ---------------------------------------------------------
func sendTextMessage(text string) {
	url := "https://api.telegram.org/bot" + tgBotToken + "/sendMessage"
	data := map[string]string{"chat_id": tgChatID, "text": text, "parse_mode": "Markdown"}
	jsonData, _ := json.Marshal(data)
	http.Post(url, "application/json", bytes.NewBuffer(jsonData))
}

func handleStatus() {
	metricsMutex.RLock()
	uptime := time.Since(startTime).Round(time.Second)
	lastCap := "Never"
	if !lastSuccessfulCapture.IsZero() {
		lastCap = lastSuccessfulCapture.Format("2006-01-02 15:04:05")
	}
	metricsMutex.RUnlock()

	status := fmt.Sprintf("📊 *termuxcam Full Status*\n\n"+
		"⏱ *Uptime:* `%s`\n"+
		"📸 *Last Capture:* `%s`\n"+
		"💾 *Disk:* `%s`\n"+
		"🧠 *Memory:* `%s`\n"+
		"🔥 *CPU Usage:* `%s`\n"+
		"🌡️ *CPU Temp:* `%s`\n"+
		"🔋 *Battery:* `%s`\n"+
		"🌐 *Local IP:* `%s`\n"+
		"🌍 *Public IP:* `%s`\n"+
		"📷 *Camera Mode:* `%d` | Motion: `%v` (threshold %d)\n"+
		"⏰ *Interval:* `%s` | Heartbeat: `%s`\n"+
		"📜 *Last Log:* `%s`",
		uptime, lastCap, getDiskUsage(), getMemoryUsage(),
		getCPUUsagePercent(), getCPUTemperature(), getBatteryInfo(),
		getLocalIP(), getPublicIP(),
		cameraMode, motionEnabled, motionThreshold,
		interval, heartbeat, getLastLogLine())

	sendTextMessage(status)
}

func startTelegramPolling(ctx context.Context) {
	logMsg("Telegram polling started - /status command active")
	offset := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
			url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30", tgBotToken, offset)
			resp, err := http.Get(url)
			if err != nil {
				time.Sleep(5 * time.Second)
				continue
			}

			var result struct {
				Ok     bool `json:"ok"`
				Result []struct {
					UpdateID int `json:"update_id"`
					Message  struct {
						Text string `json:"text"`
					} `json:"message"`
				} `json:"result"`
			}

			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()

			for _, u := range result.Result {
				offset = u.UpdateID + 1
				if strings.TrimSpace(u.Message.Text) == "/status" {
					handleStatus()
				}
			}
		}
	}
}

// --- Capture ----------------------------------------------------------
func capturePhoto(ctx context.Context, hwID, label string) (string, error) {
	if _, err := exec.LookPath("termux-camera-photo"); err != nil {
		return "", fmt.Errorf("termux-camera-photo not found")
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
	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		return false
	}
	absPhoto, err := filepath.Abs(photoPath)
	if err != nil || !strings.HasPrefix(absPhoto, absOut+string(os.PathSeparator)) {
		return false
	}

	file, err := os.Open(photoPath)
	if err != nil {
		return false
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("chat_id", tgChatID)
	writer.WriteField("caption", caption)

	part, _ := writer.CreateFormFile("photo", filepath.Base(photoPath))
	io.Copy(part, file)
	writer.Close()

	url := "https://api.telegram.org/bot" + tgBotToken + "/sendPhoto"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var r struct{ Ok bool `json:"ok"` }
	json.NewDecoder(resp.Body).Decode(&r)
	return r.Ok
}

// --- Orchestration ----------------------------------------------------
func runOnce(ctx context.Context, state motionState) motionState {
	now := time.Now()
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	nowStr := now.In(loc).Format("2006-01-02 15:04:05")

	for _, cam := range camerasForMode(cameraMode) {
		photo, err := capturePhoto(ctx, cam.hwID, cam.label)
		if err != nil {
			continue
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
						reason = fmt.Sprintf("no significant change (distance=%d/%d)", dist, hashBits)
					} else if heartbeatDue {
						reason = fmt.Sprintf("heartbeat due (distance=%d/%d)", dist, hashBits)
					} else {
						reason = fmt.Sprintf("motion detected (distance=%d/%d)", dist, hashBits)
					}
				} else {
					reason = "first capture for this camera"
				}

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
				logMsg("WARN: failed to delete local file after upload")
			} else {
				logMsg("File sent and deleted")
			}

			metricsMutex.Lock()
			lastSuccessfulCapture = time.Now()
			metricsMutex.Unlock()

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
		fmt.Println("ERROR: TG_BOT_TOKEN or TG_CHAT_ID not set")
		os.Exit(1)
	}

	// Force correct timezone
	os.Setenv("TZ", "America/Sao_Paulo")
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	time.Local = loc

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

	// Start Telegram polling for /status
	go startTelegramPolling(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam started | Binary dir: %s | Interval: %s | /status enabled", exeDir, interval))

	state = runOnce(ctx, state)
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
