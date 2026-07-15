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
	startTime         = time.Now()
	lastSuccessfulCapture time.Time
	metricsMutex      sync.RWMutex
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
		logMsg("ERROR: failed to read config, using defaults")
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

	logMsg(fmt.Sprintf("Config loaded -> interval=%s | cameraMode=%d | motion=%v | threshold=%d | heartbeat=%s",
		interval, cameraMode, motionEnabled, motionThreshold, heartbeat))
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
	default:
		return 0, fmt.Errorf("invalid duration")
	}
}

// --- Motion State -----------------------------------------------------
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
	json.Unmarshal(data, &st)
	if st.Cameras == nil {
		st.Cameras = make(map[string]camState)
	}
	return st
}

func saveState(st motionState) {
	data, _ := json.Marshal(st)
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

// --- Wake Lock --------------------------------------------------------
func acquireWakeLock() {
	if _, err := exec.LookPath("termux-wake-lock"); err == nil {
		exec.Command("termux-wake-lock").Run()
	}
}

func releaseWakeLock() {
	exec.Command("termux-wake-unlock").Run()
}

// --- Capture ----------------------------------------------------------
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
		os.Remove(outfile)
		return "", err
	}

	if info, err := os.Stat(outfile); err != nil || info.Size() == 0 {
		os.Remove(outfile)
		return "", fmt.Errorf("empty capture")
	}

	logMsg(fmt.Sprintf("Captured successfully: %s", outfile))
	return outfile, nil
}

// --- Telegram ---------------------------------------------------------
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
		return false
	}
	defer resp.Body.Close()

	var r struct{ Ok bool `json:"ok"` }
	json.NewDecoder(resp.Body).Decode(&r)
	return r.Ok
}

// Send text message to Telegram
func sendTextMessage(text string) {
	url := "https://api.telegram.org/bot" + tgBotToken + "/sendMessage"
	data := map[string]string{
		"chat_id": tgChatID,
		"text":    text,
	}
	jsonData, _ := json.Marshal(data)

	http.Post(url, "application/json", bytes.NewBuffer(jsonData))
}

// Get disk usage info
func getDiskUsage() string {
	var used, total string
	if out, err := exec.Command("df", "-h", outputDir).Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 1 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 4 {
				used = fields[2]
				total = fields[1]
			}
		}
	}
	if used == "" {
		return "Unknown"
	}
	return fmt.Sprintf("%s / %s", used, total)
}

// Handle /status command
func handleStatus() {
	metricsMutex.RLock()
	uptime := time.Since(startTime).Round(time.Second)
	lastCapture := "Never"
	if !lastSuccessfulCapture.IsZero() {
		lastCapture = lastSuccessfulCapture.Format("2006-01-02 15:04:05")
	}
	metricsMutex.RUnlock()

	disk := getDiskUsage()

	status := fmt.Sprintf("📊 *termuxcam Status*\n\n"+
		"🕒 Uptime: %s\n"+
		"📸 Last Capture: %s\n"+
		"💾 Disk Usage: %s\n"+
		"📡 Interval: %s\n"+
		"📷 Camera Mode: %d\n"+
		"🔍 Motion: %v",
		uptime, lastCapture, disk, interval, cameraMode, motionEnabled)

	sendTextMessage(status)
}

// Telegram polling for commands
func startTelegramPolling(ctx context.Context) {
	logMsg("Telegram polling started for /status command")

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

			for _, update := range result.Result {
				offset = update.UpdateID + 1
				if update.Message.Text == "/status" {
					handleStatus()
				}
			}
		}
	}
}

// --- Main Orchestration -----------------------------------------------
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
				logMsg("WARN: could not compute hash, sending anyway")
			} else {
				newHash = h
				if hadPrev {
					dist := hammingDistance(h, prev.Hash)
					heartbeatDue := heartbeat > 0 && !prev.LastSent.IsZero() && now.Sub(prev.LastSent) >= heartbeat

					if dist < motionThreshold && !heartbeatDue {
						shouldSend = false
						reason = fmt.Sprintf("no significant change (dist=%d)", dist)
					} else if heartbeatDue {
						reason = "heartbeat triggered"
					} else {
						reason = fmt.Sprintf("motion detected (dist=%d)", dist)
					}
				} else {
					reason = "first capture"
				}
				state.Cameras[cam.label] = camState{Hash: newHash, LastSent: prev.LastSent}
			}
		}

		if !shouldSend {
			logMsg(fmt.Sprintf("Skipped upload (%s): %s", cam.label, reason))
			os.Remove(photo)
			continue
		}

		caption := fmt.Sprintf("%s camera: %s", strings.Title(cam.label), nowStr)

		if sendToTelegram(ctx, photo, caption) {
			os.Remove(photo)
			logMsg("File sent and deleted successfully")

			// Update last successful capture time
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

	// Start Telegram polling for /status in background
	go startTelegramPolling(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam started | Interval: %s | /status command enabled", interval))

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
