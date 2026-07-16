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
	Version        = "0.2.0"
	configFileName = "termuxcam.conf"
	stateFileName  = "motion_state.json"
	captureTimeout = 45 * time.Second
	uploadTimeout  = 30 * time.Second

	minInterval = 1 * time.Minute
	maxInterval = 24 * time.Hour

	hashGridW = 9
	hashGridH = 8
	hashBits  = 64

	defaultMotionEnabled   = true
	defaultMotionThreshold = 6
	defaultHeartbeat       = 0

	configFileMode = 0o600
	dirMode        = 0o700
	fileMode       = 0o600
)

// --- Physical hardware camera IDs ------------------------------------------
//
// What `termux-camera-photo -c <id>` actually expects. This is a property of
// the DEVICE, not of this app's config — confirm with `termux-camera-info`
// and update if different on your phone. Nothing else in this file ever
// touches a raw ID directly; everything works off the semantic "front"/
// "back" labels via camSpec/camerasForMode.
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

	httpClient = &http.Client{
		Timeout: uploadTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	// Metrics
	startTime             = time.Now()
	lastSuccessfulCapture time.Time
	capturesToday         int
	totalCaptures         int
	failedUploadsToday    int
	lastError             string
	metricsMutex          sync.RWMutex
	lastMidnight          = time.Now().Truncate(24 * time.Hour)

	// Cross-goroutine coordination for hot-reload and manual /photo
	// requests. Only main() ever mutates the ticker or the motion `state`
	// map, so both are funneled through channels rather than touched
	// directly from the Telegram polling goroutine.
	reloadCh chan struct{}
	photoCh  chan struct{}
)

// --- Config (thread-safe: read from the polling goroutine, written on
// SIGHUP from a signal-handling goroutine, read every tick from main) ------

type appConfig struct {
	Interval        time.Duration
	CameraMode      int // 0=back, 1=front, 2=both
	MotionEnabled   bool
	MotionThreshold int
	Heartbeat       time.Duration
}

var (
	cfg      = appConfig{Interval: 5 * time.Minute, CameraMode: 1, MotionEnabled: defaultMotionEnabled, MotionThreshold: defaultMotionThreshold, Heartbeat: defaultHeartbeat}
	cfgMutex sync.RWMutex
)

func getConfig() appConfig {
	cfgMutex.RLock()
	defer cfgMutex.RUnlock()
	return cfg
}

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

func cameraModeLabel(mode int) string {
	switch mode {
	case 0:
		return "back"
	case 1:
		return "front"
	case 2:
		return "both"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
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

	os.MkdirAll(outputDir, dirMode)
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err == nil {
		f.WriteString(line + "\n")
		f.Close()
	}
}

func getLastLogLine() string {
	data, err := os.ReadFile(logFilePath)
	if err != nil || len(data) == 0 {
		return "No logs yet"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return lines[len(lines)-1]
}

func getLastLogs(n int) string {
	data, err := os.ReadFile(logFilePath)
	if err != nil || len(data) == 0 {
		return "No logs yet"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// --- System information -----------------------------------------------

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

func getFolderUsage() string {
	out, _ := exec.Command("du", "-sh", outputDir).Output()
	parts := strings.Fields(string(out))
	if len(parts) > 0 {
		return parts[0]
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

func getDeviceInfo() string {
	model, _ := exec.Command("getprop", "ro.product.model").Output()
	android, _ := exec.Command("getprop", "ro.build.version.release").Output()
	return fmt.Sprintf("%s (Android %s)", strings.TrimSpace(string(model)), strings.TrimSpace(string(android)))
}

func getWifiStatus() string {
	out, err := exec.Command("termux-wifi-connectioninfo").Output()
	if err != nil {
		return "N/A"
	}
	var wifi struct {
		SupplicantState string `json:"supplicantState"`
		Rssi            int    `json:"rssi"`
	}
	json.Unmarshal(out, &wifi)
	if wifi.SupplicantState == "COMPLETED" {
		return fmt.Sprintf("Connected (signal: %d dBm)", wifi.Rssi)
	}
	return "Disconnected"
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

// --- Config file loading -------------------------------------------------

// stripInlineComment removes a trailing "# ..." comment from a config value.
func stripInlineComment(s string) string {
	if idx := strings.Index(s, "#"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// loadConfig reads termuxcam.conf (creating a default one on first run) and
// atomically replaces the shared `cfg` under cfgMutex. Safe to call again at
// any time — e.g. on SIGHUP — from any goroutine.
func loadConfig() {
	configPath := filepath.Join(exeDir, configFileName)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultCfg := `# termuxcam configuration

# Capture interval — a number followed by 'm' (minutes) or 'h' (hours)
capture=5m

# Camera mode: 0 = back only, 1 = front only, 2 = both
camera=1

# Motion detection: skip uploading (and delete) a capture that looks
# essentially identical to the previous one from the same camera.
motion=true

# How different two frames must be to count as "changed", out of 64.
# Lower = more sensitive. 4-10 is reasonable for a static indoor camera.
motion_threshold=6

# Force an upload at least this often even with no detected motion.
# 0 disables this (motion-triggered uploads only).
heartbeat=1h
`
		os.WriteFile(configPath, []byte(defaultCfg), configFileMode)
		logMsg("Created default config: " + configPath)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		logMsg("ERROR: failed to read config, keeping current values: " + err.Error())
		return
	}

	// Parse into locals first (seeded with the current values), then commit
	// atomically — a reload with a malformed line should never leave the
	// live config half-updated.
	next := getConfig()

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, raw := range lines {
		line := stripInlineComment(raw)
		if line == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "capture":
			if d, err := parseDuration(val); err == nil && d >= minInterval && d <= maxInterval {
				next.Interval = d
			}
		case "camera":
			if c, err := strconv.Atoi(val); err == nil && c >= 0 && c <= 2 {
				next.CameraMode = c
			}
		case "motion":
			if b, err := strconv.ParseBool(val); err == nil {
				next.MotionEnabled = b
			}
		case "motion_threshold":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= hashBits {
				next.MotionThreshold = n
			}
		case "heartbeat":
			if val == "0" || val == "0h" || val == "0m" {
				next.Heartbeat = 0
			} else if d, err := parseDuration(val); err == nil {
				next.Heartbeat = d
			}
		}
	}

	cfgMutex.Lock()
	cfg = next
	cfgMutex.Unlock()

	labels := make([]string, 0, 2)
	for _, c := range camerasForMode(next.CameraMode) {
		labels = append(labels, fmt.Sprintf("%s(hw=%s)", c.label, c.hwID))
	}
	logMsg(fmt.Sprintf(
		"Config loaded -> interval=%s | camera=%s | motion=%v threshold=%d/%d | heartbeat=%s | file=%s",
		next.Interval, strings.Join(labels, ", "), next.MotionEnabled, next.MotionThreshold, hashBits, next.Heartbeat, configPath,
	))
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(s, "h"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "h"))
		if err != nil {
			return 0, fmt.Errorf("invalid hour value %q", s)
		}
		return time.Duration(n) * time.Hour, nil
	case strings.HasSuffix(s, "m"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "m"))
		if err != nil {
			return 0, fmt.Errorf("invalid minute value %q", s)
		}
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("duration %q missing 'h' or 'm' suffix", s)
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
	if err == nil {
		json.Unmarshal(data, &st)
	}
	if st.Cameras == nil {
		st.Cameras = make(map[string]camState)
	}
	return st
}

func saveState(st motionState) {
	data, _ := json.Marshal(st)
	os.WriteFile(statePath, data, fileMode)
}

// computeDHash produces a 64-bit difference hash of the image at path, using
// only the stdlib (no external imaging libraries): downsample to a small
// grayscale grid via nearest-neighbor sampling, then encode whether each
// pixel is brighter than its right neighbor.
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
	if _, err := exec.LookPath("termux-wake-lock"); err == nil {
		exec.Command("termux-wake-lock").Run()
	}
}

func releaseWakeLock() {
	exec.Command("termux-wake-unlock").Run()
}

// --- Telegram commands --------------------------------------------------

func handleHelp() {
	helpText := "📋 *Available commands*\n\n" +
		"/status - Show full system status\n" +
		"/config - Show current configuration\n" +
		"/photo - Take a photo right now\n" +
		"/log - Show last 15 log lines\n" +
		"/restart - Restart termuxcam service\n" +
		"/help - Show this help message"

	sendTextMessage(helpText)
}

// escapeForStatus neutralizes Telegram legacy-Markdown special characters in
// free-form text (log lines, error messages) so they can't break the
// formatting — or make the whole message fail to send.
func escapeForStatus(s string) string {
	replacer := strings.NewReplacer(
		"`", "'",
		"*", "•",
		"_", "-",
		"[", "(",
		"]", ")",
	)
	return replacer.Replace(s)
}

func handleStatus() {
	c := getConfig()

	metricsMutex.RLock()
	uptime := time.Since(startTime).Round(time.Second)
	lastCap := "Never"
	if !lastSuccessfulCapture.IsZero() {
		lastCap = lastSuccessfulCapture.Format("2006-01-02 15:04:05")
	}
	failedToday := failedUploadsToday
	capToday := capturesToday
	capTotal := totalCaptures
	errText := lastError
	metricsMutex.RUnlock()

	if errText == "" {
		errText = "(none)"
	}
	nextCapture := time.Now().Add(c.Interval).Format("15:04")
	lastLog := getLastLogLine()

	const divider = "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬"

	status := fmt.Sprintf(
		"📊 *termuxcam* `v%s`\n"+
			"%s\n\n"+

			"*⏱ SYSTEM*\n"+
			"Uptime: `%s`\n"+
			"Captures today: `%d`  ·  total: `%d`\n"+
			"Last capture: `%s`\n"+
			"Failures today: `%d`\n\n"+

			"*📷 CAMERA*\n"+
			"Mode: `%s`  ·  Motion: `%v`\n"+
			"Interval: `%s`\n"+
			"Next capture: `%s`\n"+
			"Heartbeat: `%s`\n\n"+

			"*💾 RESOURCES*\n"+
			"Disk: `%s`\n"+
			"Folder: `%s`\n"+
			"Memory: `%s`\n"+
			"CPU: `%s`  ·  Temp: `%s`\n\n"+

			"*🔋 DEVICE*\n"+
			"Battery: `%s`\n"+
			"Model: `%s`\n\n"+

			"*🌐 NETWORK*\n"+
			"Local IP: `%s`\n"+
			"Public IP: `%s`\n"+
			"WiFi: `%s`\n\n"+

			"*⚠️ LAST ERROR*\n"+
			"`%s`\n\n"+

			"*📜 LAST LOG*\n"+
			"`%s`",
		Version,
		divider,

		uptime,
		capToday, capTotal,
		lastCap,
		failedToday,

		cameraModeLabel(c.CameraMode), c.MotionEnabled,
		c.Interval,
		nextCapture,
		c.Heartbeat,

		getDiskUsage(),
		getFolderUsage(),
		getMemoryUsage(),
		getCPUUsagePercent(), getCPUTemperature(),

		getBatteryInfo(),
		getDeviceInfo(),

		getLocalIP(),
		getPublicIP(),
		getWifiStatus(),

		escapeForStatus(errText),
		escapeForStatus(lastLog),
	)

	sendTextMessage(status)
}

// handleConfig shows the live, currently-applied configuration — as
// distinct from /status, this is meant as a quick way to confirm what a
// SIGHUP reload actually picked up.
func handleConfig() {
	c := getConfig()

	labels := make([]string, 0, 2)
	for _, cam := range camerasForMode(c.CameraMode) {
		labels = append(labels, cam.label)
	}

	heartbeatStr := "disabled"
	if c.Heartbeat > 0 {
		heartbeatStr = c.Heartbeat.String()
	}

	msg := fmt.Sprintf(
		"⚙️ *Current configuration*\n\n"+
			"Capture interval: `%s`\n"+
			"Camera mode: `%d` (%s)\n"+
			"Motion detection: `%v`\n"+
			"Motion threshold: `%d/%d`\n"+
			"Heartbeat: `%s`\n\n"+
			"_Edit %s and send SIGHUP (or /restart) to apply changes._",
		c.Interval,
		c.CameraMode, strings.Join(labels, "+"),
		c.MotionEnabled,
		c.MotionThreshold, hashBits,
		heartbeatStr,
		filepath.Join(exeDir, configFileName),
	)

	sendTextMessage(msg)
}

// handlePhoto queues an immediate, unconditional capture. The actual work
// happens on the main goroutine (see main's select loop) since it needs to
// mutate the shared motion `state` map, which is not safe to touch
// concurrently from here.
func handlePhoto() {
	select {
	case photoCh <- struct{}{}:
		sendTextMessage("📸 Taking photo now...")
	default:
		sendTextMessage("⏳ A photo request is already in progress, please wait.")
	}
}

func handleRestart() {
	sendTextMessage("🔄 Restarting termuxcam...")
	logMsg("Restart requested via /restart command")
	time.Sleep(1 * time.Second)
	os.Exit(0)
}

func handleLog() {
	logs := getLastLogs(15)
	sendTextMessage("📜 *Last 15 log lines:*\n\n" + logs)
}

func sendTextMessage(text string) {
	url := "https://api.telegram.org/bot" + tgBotToken + "/sendMessage"
	data := map[string]string{"chat_id": tgChatID, "text": text, "parse_mode": "Markdown"}
	jsonData, _ := json.Marshal(data)
	http.Post(url, "application/json", bytes.NewBuffer(jsonData))
}

func startTelegramPolling(ctx context.Context) {
	logMsg("Telegram polling started - commands: /status, /config, /photo, /log, /restart, /help")
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
				text := strings.TrimSpace(u.Message.Text)

				switch text {
				case "/status":
					handleStatus()
				case "/config":
					handleConfig()
				case "/photo":
					handlePhoto()
				case "/log":
					handleLog()
				case "/restart":
					handleRestart()
				case "/help":
					handleHelp()
				}
			}
		}
	}
}

// --- Capture ------------------------------------------------------------

func capturePhoto(ctx context.Context, hwID, label string) (string, error) {
	ts := time.Now().Format("20060102_150405")
	outfile := filepath.Join(outputDir, fmt.Sprintf("capture_%s_%s.jpg", label, ts))

	cctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "termux-camera-photo", "-c", hwID, outfile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("Capture failed (%s): %s", label, stderr.String()))
		metricsMutex.Lock()
		lastError = stderr.String()
		metricsMutex.Unlock()
		return "", err
	}

	info, err := os.Stat(outfile)
	if err != nil || info.Size() == 0 {
		os.Remove(outfile)
		return "", fmt.Errorf("empty capture")
	}

	metricsMutex.Lock()
	totalCaptures++
	capturesToday++
	metricsMutex.Unlock()

	logMsg(fmt.Sprintf("Captured successfully: %s", outfile))
	return outfile, nil
}

func sendToTelegram(ctx context.Context, photoPath, caption string) bool {
	absOut, _ := filepath.Abs(outputDir)
	absPhoto, _ := filepath.Abs(photoPath)
	if !strings.HasPrefix(absPhoto, absOut+string(os.PathSeparator)) {
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
		metricsMutex.Lock()
		failedUploadsToday++
		metricsMutex.Unlock()
		return false
	}
	defer resp.Body.Close()

	var r struct {
		Ok bool `json:"ok"`
	}
	json.NewDecoder(resp.Body).Decode(&r)

	if !r.Ok {
		metricsMutex.Lock()
		failedUploadsToday++
		metricsMutex.Unlock()
	}
	return r.Ok
}

// runOnce captures from every camera selected by cfg.CameraMode. When force
// is true (manual /photo request), motion detection is bypassed and the
// frame is always uploaded — but the motion baseline is still updated, so
// the next automatic cycle compares against this fresh frame rather than a
// stale one.
func runOnce(ctx context.Context, cfgSnapshot appConfig, state motionState, force bool) motionState {
	now := time.Now()
	nowStr := now.Format("2006-01-02 15:04:05")

	for _, cam := range camerasForMode(cfgSnapshot.CameraMode) {
		photo, err := capturePhoto(ctx, cam.hwID, cam.label)
		if err != nil {
			continue
		}

		shouldSend := true
		var newHash uint64
		prev, hadPrev := state.Cameras[cam.label]

		if cfgSnapshot.MotionEnabled {
			if h, herr := computeDHash(photo); herr == nil {
				newHash = h
				if hadPrev && !force {
					dist := hammingDistance(h, prev.Hash)
					heartbeatDue := cfgSnapshot.Heartbeat > 0 && !prev.LastSent.IsZero() && now.Sub(prev.LastSent) >= cfgSnapshot.Heartbeat
					if dist < cfgSnapshot.MotionThreshold && !heartbeatDue {
						shouldSend = false
					}
				}
				state.Cameras[cam.label] = camState{Hash: newHash, LastSent: prev.LastSent}
			}
		}

		if !shouldSend {
			os.Remove(photo)
			continue
		}

		suffix := ""
		if force {
			suffix = " (manual)"
		}
		caption := fmt.Sprintf("%s camera: %s%s", strings.Title(cam.label), nowStr, suffix)

		if sendToTelegram(ctx, photo, caption) {
			os.Remove(photo)
			metricsMutex.Lock()
			lastSuccessfulCapture = time.Now()
			metricsMutex.Unlock()
			if cfgSnapshot.MotionEnabled {
				cs := state.Cameras[cam.label]
				cs.LastSent = now
				state.Cameras[cam.label] = cs
			}
		}
	}
	return state
}

func main() {
	if tgBotToken == "" || tgChatID == "" {
		fmt.Println("ERROR: TG_BOT_TOKEN or TG_CHAT_ID not set")
		os.Exit(1)
	}

	os.Setenv("TZ", "America/Sao_Paulo")
	if loc, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		time.Local = loc
	}

	exeDir = getExeDir()
	outputDir = filepath.Join(exeDir, "camera_captures")
	logFilePath = filepath.Join(outputDir, "capture.log")
	statePath = filepath.Join(outputDir, stateFileName)

	os.MkdirAll(outputDir, dirMode)

	reloadCh = make(chan struct{}, 1)
	photoCh = make(chan struct{}, 1)

	loadConfig()
	state := loadState()

	acquireWakeLock()
	defer releaseWakeLock()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// SIGHUP triggers a config reload without restarting the process.
	// loadConfig() itself is safe to call from any goroutine (it commits
	// atomically under cfgMutex); only the resulting ticker interval change
	// needs to be applied from main, hence the channel handoff.
	go func() {
		sigHup := make(chan os.Signal, 1)
		signal.Notify(sigHup, syscall.SIGHUP)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigHup:
				logMsg("SIGHUP received, reloading config")
				loadConfig()
				select {
				case reloadCh <- struct{}{}:
				default:
				}
			}
		}
	}()

	go startTelegramPolling(ctx)
	go startMidnightReset()

	ticker := time.NewTicker(getConfig().Interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam v%s started successfully", Version))

	state = runOnce(ctx, getConfig(), state, false)
	saveState(state)

	for {
		select {
		case <-ticker.C:
			state = runOnce(ctx, getConfig(), state, false)
			saveState(state)
		case <-photoCh:
			state = runOnce(ctx, getConfig(), state, true)
			saveState(state)
		case <-reloadCh:
			newInterval := getConfig().Interval
			ticker.Reset(newInterval)
			logMsg(fmt.Sprintf("Ticker reset after config reload, new interval=%s", newInterval))
		case <-ctx.Done():
			logMsg("Shutting down on signal")
			return
		}
	}
}

// --- Midnight reset -----------------------------------------------------

func startMidnightReset() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		now := time.Now()
		if now.Truncate(24*time.Hour).After(lastMidnight) {
			metricsMutex.Lock()
			capturesToday = 0
			failedUploadsToday = 0
			lastMidnight = now.Truncate(24 * time.Hour)
			metricsMutex.Unlock()
			logMsg("Daily counters reset at midnight")
		}
	}
}
