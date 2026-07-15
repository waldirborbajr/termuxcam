package main

import (
	"bytes"
	"context"
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
)

var (
	exeDir       string
	outputDir    string
	logFilePath  string
	tgBotToken   = os.Getenv("TG_BOT_TOKEN")
	tgChatID     = os.Getenv("TG_CHAT_ID")
	interval     = 5 * time.Minute // fallback
	cameraMode   = 1               // 0=front, 1=back, 2=both
)

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

	os.MkdirAll(outputDir, 0755)
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		f.WriteString(line + "\n")
	}
}

func loadConfig() {
	configPath := filepath.Join(exeDir, configFileName)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultCfg := `# termuxcam configuration
# capture=15m   (m = minutes, h = hours)
capture=5m

# camera=0  → Front only
# camera=1  → Back only (default)
# camera=2  → Both cameras
camera=1
`
		os.WriteFile(configPath, []byte(defaultCfg), 0644)
		logMsg("Created default config: " + configPath)
	}

	data, _ := os.ReadFile(configPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
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
			if d, err := parseDuration(val); err == nil {
				interval = d
			}
		case "camera":
			if c, err := strconv.Atoi(val); err == nil && c >= 0 && c <= 2 {
				cameraMode = c
			}
		}
	}

	logMsg(fmt.Sprintf("Config loaded → interval=%s | cameraMode=%d | config=%s", interval, cameraMode, configPath))
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasSuffix(s, "h") {
		n, _ := strconv.Atoi(strings.TrimSuffix(s, "h"))
		return time.Duration(n) * time.Hour, nil
	} else if strings.HasSuffix(s, "m") {
		n, _ := strconv.Atoi(strings.TrimSuffix(s, "m"))
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("invalid duration")
}

func acquireWakeLock() {
	exec.Command("termux-wake-lock").Run()
}

func releaseWakeLock() {
	exec.Command("termux-wake-unlock").Run()
}

func capturePhoto(cameraID string) (string, error) {
	ts := time.Now().Format("20060102_150405")
	outfile := filepath.Join(outputDir, fmt.Sprintf("capture_%s_cam%s.jpg", ts, cameraID))

	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "termux-camera-photo", "-c", cameraID, outfile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("Capture failed (cam %s): %s", cameraID, stderr.String()))
		os.Remove(outfile)
		return "", err
	}

	if info, err := os.Stat(outfile); err != nil || info.Size() == 0 {
		os.Remove(outfile)
		return "", fmt.Errorf("empty file")
	}

	logMsg(fmt.Sprintf("Captured successfully: %s", outfile))
	return outfile, nil
}

func sendToTelegram(photoPath, caption string) bool {
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

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", tgBotToken)
	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: uploadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var r struct{ Ok bool }
	json.NewDecoder(resp.Body).Decode(&r)
	return r.Ok
}

func runOnce() {
	now := time.Now().Format("2006-01-02 15:04:05")

	cams := []string{}
	if cameraMode == 0 || cameraMode == 2 {
		cams = append(cams, "0")
	}
	if cameraMode == 1 || cameraMode == 2 {
		cams = append(cams, "1")
	}

	for _, id := range cams {
		name := map[string]string{"0": "Front Camera", "1": "Back Camera"}[id]

		photo, err := capturePhoto(id)
		if err != nil {
			continue
		}

		caption := fmt.Sprintf("%s: %s", name, now)

		if sendToTelegram(photo, caption) {
			os.Remove(photo)
			logMsg("File sent and deleted")
		} else {
			logMsg("Upload failed - keeping local file")
		}
	}
}

func main() {
	if tgBotToken == "" || tgChatID == "" {
		fmt.Println("❌ TG_BOT_TOKEN or TG_CHAT_ID not set")
		os.Exit(1)
	}

	exeDir = getExeDir()
	outputDir = filepath.Join(exeDir, "camera_captures")
	logFilePath = filepath.Join(outputDir, "capture.log")

	loadConfig()

	acquireWakeLock()
	defer releaseWakeLock()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam started | Binary dir: %s | Interval: %s", exeDir, interval))

	runOnce() // primeira captura imediata

	for {
		select {
		case <-ticker.C:
			runOnce()
		case sig := <-sigCh:
			logMsg(fmt.Sprintf("Shutting down on signal: %v", sig))
			return
		}
	}
}
