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
	Version        = "0.1.1"
	configFileName = "termuxcam.conf"
	stateFileName  = "motion_state.json"
	captureTimeout = 45 * time.Second
	uploadTimeout  = 30 * time.Second

	minInterval = 1 * time.Minute
	maxInterval = 24 * time.Hour

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
	cameraMode      = 1
	motionEnabled   = defaultMotionEnabled
	motionThreshold = defaultMotionThreshold
	heartbeat       time.Duration

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

// System Information
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
		return fmt.Sprintf("Connected (Signal: %d dBm)", wifi.Rssi)
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

// Config
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
capture=5m
camera=1
motion=true
motion_threshold=6
heartbeat=1h
`
		os.WriteFile(configPath, []byte(defaultCfg), configFileMode)
		logMsg("Created default config: " + configPath)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		logMsg("ERROR: failed to read config")
		return
	}

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
				interval = d
			}
		case "camera":
			if c, err := strconv.Atoi(val); err == nil && c >= 0 && c <= 2 {
				cameraMode = c
			}
		case "motion":
			if b, err := strconv.ParseBool(val); err == nil {
				motionEnabled = b
			}
		case "motion_threshold":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= hashBits {
				motionThreshold = n
			}
		case "heartbeat":
			if val == "0" || val == "0h" || val == "0m" {
				heartbeat = 0
			} else if d, err := parseDuration(val); err == nil {
				heartbeat = d
			}
		}
	}
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(s, "h"):
		n, _ := strconv.Atoi(strings.TrimSuffix(s, "h"))
		return time.Duration(n) * time.Hour, nil
	case strings.HasSuffix(s, "m"):
		n, _ := strconv.Atoi(strings.TrimSuffix(s, "m"))
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("invalid duration")
}

// Motion State
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

// dHash
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

// Wake Lock
func acquireWakeLock() {
	if _, err := exec.LookPath("termux-wake-lock"); err == nil {
		exec.Command("termux-wake-lock").Run()
	}
}

func releaseWakeLock() {
	exec.Command("termux-wake-unlock").Run()
}

// Telegram Commands
func handleHelp() {
	helpText := "📋 *Available Commands*\n\n" +
		"/status - Show full system status\n" +
		"/log - Show last 15 log lines\n" +
		"/restart - Restart termuxcam service\n" +
		"/help - Show this help message"

	sendTextMessage(helpText)
}

func handleStatus() {
	metricsMutex.RLock()
	uptime := time.Since(startTime).Round(time.Second)
	lastCap := "Never"
	if !lastSuccessfulCapture.IsZero() {
		lastCap = lastSuccessfulCapture.Format("2006-01-02 15:04:05")
	}
	nextCapture := time.Now().Add(interval).Format("15:04")
	metricsMutex.RUnlock()

	status := fmt.Sprintf("📊 *termuxcam v%s - Full Status*\n\n"+
		"⏱ *Uptime:* `%s`\n"+
		"📸 *Captures Today:* `%d` | Total: `%d`\n"+
		"📸 *Last Capture:* `%s`\n"+
		"💾 *Disk:* `%s`\n"+
		"📁 *Folder Usage:* `%s`\n"+
		"🧠 *Memory:* `%s`\n"+
		"🔥 *CPU Usage:* `%s`\n"+
		"🌡️ *CPU Temp:* `%s`\n"+
		"🔋 *Battery:* `%s`\n"+
		"🌐 *Local IP:* `%s`\n"+
		"🌍 *Public IP:* `%s`\n"+
		"📡 *WiFi:* `%s`\n"+
		"📱 *Device:* `%s`\n"+
		"📷 *Camera Mode:* `%d` | Motion: `%v`\n"+
		"⏰ *Interval:* `%s` | Next: `%s`\n"+
		"❤️ *Heartbeat:* `%s`\n"+
		"❌ *Failed Uploads Today:* `%d`\n"+
		"⚠️ *Last Error:* `%s`\n"+
		"📜 *Last Log:* `%s`",
		Version, uptime, capturesToday, totalCaptures, lastCap,
		getDiskUsage(), getFolderUsage(), getMemoryUsage(),
		getCPUUsagePercent(), getCPUTemperature(), getBatteryInfo(),
		getLocalIP(), getPublicIP(), getWifiStatus(), getDeviceInfo(),
		cameraMode, motionEnabled, interval, nextCapture, heartbeat,
		failedUploadsToday, lastError, getLastLogLine())

	sendTextMessage(status)
}

func handleRestart() {
	sendTextMessage("🔄 Restarting termuxcam...")
	logMsg("Restart requested via /restart command")
	time.Sleep(1 * time.Second)
	os.Exit(0)
}

func handleLog() {
	logs := getLastLogs(15)
	sendTextMessage("📜 *Last 15 Log Lines:*\n\n" + logs)
}

func sendTextMessage(text string) {
	url := "https://api.telegram.org/bot" + tgBotToken + "/sendMessage"
	data := map[string]string{"chat_id": tgChatID, "text": text, "parse_mode": "Markdown"}
	jsonData, _ := json.Marshal(data)
	http.Post(url, "application/json", bytes.NewBuffer(jsonData))
}

func startTelegramPolling(ctx context.Context) {
	logMsg("Telegram polling started - commands: /status, /log, /restart, /help")
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

// Midnight Reset
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

// Capture
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
		lastError = stderr.String()
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

	var r struct{ Ok bool `json:"ok"` }
	json.NewDecoder(resp.Body).Decode(&r)

	if !r.Ok {
		metricsMutex.Lock()
		failedUploadsToday++
		metricsMutex.Unlock()
	}
	return r.Ok
}

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
		var newHash uint64
		prev, hadPrev := state.Cameras[cam.label]

		if motionEnabled {
			if h, herr := computeDHash(photo); herr == nil {
				newHash = h
				if hadPrev {
					dist := hammingDistance(h, prev.Hash)
					heartbeatDue := heartbeat > 0 && !prev.LastSent.IsZero() && now.Sub(prev.LastSent) >= heartbeat
					if dist < motionThreshold && !heartbeatDue {
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

		caption := fmt.Sprintf("%s camera: %s", strings.Title(cam.label), nowStr)

		if sendToTelegram(ctx, photo, caption) {
			os.Remove(photo)
			metricsMutex.Lock()
			lastSuccessfulCapture = time.Now()
			metricsMutex.Unlock()
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
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	time.Local = loc

	exeDir = getExeDir()
	outputDir = filepath.Join(exeDir, "camera_captures")
	logFilePath = filepath.Join(outputDir, "capture.log")
	statePath = filepath.Join(outputDir, stateFileName)

	os.MkdirAll(outputDir, dirMode)

	loadConfig()
	state := loadState()

	acquireWakeLock()
	defer releaseWakeLock()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go startTelegramPolling(ctx)
	go startMidnightReset()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam v%s started successfully", Version))

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

// Midnight Reset
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
