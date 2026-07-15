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
	configFile     = "termuxcam.conf"
	captureTimeout = 45 * time.Second
	uploadTimeout  = 30 * time.Second
)

var (
	homeDir, _   = os.UserHomeDir()
	outputDir    = filepath.Join(homeDir, "camera_captures")
	logFilePath  = filepath.Join(outputDir, "capture.log")
	tgBotToken   = os.Getenv("TG_BOT_TOKEN")
	tgChatID     = os.Getenv("TG_CHAT_ID")
	interval     = 5 * time.Minute // default
	cameraMode   = 1               // 0=front, 1=back, 2=both
)

type Config struct {
	Capture string `json:"capture"`
	Camera  int    `json:"camera"`
}

func logMsg(msg string) {
	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] %s", ts, msg)
	fmt.Println(line)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return
	}
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(line + "\n")
}

func loadConfig() {
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultCfg := `capture=5m
camera=1`
		os.WriteFile(configFile, []byte(defaultCfg), 0644)
		logMsg("Created default " + configFile)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		logMsg("Failed to read config: " + err.Error())
		return
	}

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
		value := strings.TrimSpace(parts[1])

		switch key {
		case "capture":
			d, err := parseDuration(value)
			if err == nil {
				interval = d
			} else {
				logMsg("Invalid capture duration: " + value)
			}
		case "camera":
			if c, err := strconv.Atoi(value); err == nil {
				if c >= 0 && c <= 2 {
					cameraMode = c
				}
			}
		}
	}
	logMsg(fmt.Sprintf("Config loaded: capture=%s, camera=%d", interval, cameraMode))
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasSuffix(s, "h") {
		num := strings.TrimSuffix(s, "h")
		if v, err := strconv.Atoi(num); err == nil {
			return time.Duration(v) * time.Hour, nil
		}
	} else if strings.HasSuffix(s, "m") {
		num := strings.TrimSuffix(s, "m")
		if v, err := strconv.Atoi(num); err == nil {
			return time.Duration(v) * time.Minute, nil
		}
	}
	return 0, fmt.Errorf("invalid duration")
}

func acquireWakeLock() {
	if err := exec.Command("termux-wake-lock").Run(); err != nil {
		logMsg("failed to acquire wake-lock")
	}
}

func releaseWakeLock() {
	exec.Command("termux-wake-unlock").Run()
}

func capturePhoto(cameraID string) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}

	ts := time.Now().Format("20060102_150405")
	outfile := filepath.Join(outputDir, fmt.Sprintf("capture_%s_cam%s.jpg", ts, cameraID))

	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "termux-camera-photo", "-c", cameraID, outfile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("capture failed (cam %s): %s", cameraID, stderr.String()))
		os.Remove(outfile)
		return "", err
	}

	info, err := os.Stat(outfile)
	if err != nil || info.Size() == 0 {
		os.Remove(outfile)
		return "", fmt.Errorf("empty or missing capture")
	}

	logMsg(fmt.Sprintf("captured -> %s (%d bytes)", outfile, info.Size()))
	return outfile, nil
}

func sendToTelegram(photoPath, caption string) bool {
	file, err := os.Open(photoPath)
	if err != nil {
		logMsg("could not open photo")
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
		logMsg("Telegram send error: " + err.Error())
		return false
	}
	defer resp.Body.Close()

	var tgResp struct{ Ok bool }
	json.NewDecoder(resp.Body).Decode(&tgResp)

	if !tgResp.Ok {
		logMsg("Telegram response not OK")
		return false
	}

	logMsg("sent to Telegram with caption")
	return true
}

func runOnce() {
	now := time.Now().Format("2006-01-02 15:04:05")

	cameras := []string{}
	if cameraMode == 0 || cameraMode == 2 {
		cameras = append(cameras, "0")
	}
	if cameraMode == 1 || cameraMode == 2 {
		cameras = append(cameras, "1")
	}

	for _, camID := range cameras {
		name := "Front Camera"
		if camID == "1" {
			name = "Back Camera"
		}

		photo, err := capturePhoto(camID)
		if err != nil {
			continue
		}

		caption := fmt.Sprintf("%s: %s", name, now)

		if sendToTelegram(photo, caption) {
			os.Remove(photo)
			logMsg("deleted local file")
		} else {
			logMsg("keeping local file (upload failed)")
		}
	}
}

func main() {
	if tgBotToken == "" || tgChatID == "" {
		logMsg("TG_BOT_TOKEN or TG_CHAT_ID not set")
		os.Exit(1)
	}

	loadConfig()
	acquireWakeLock()
	defer releaseWakeLock()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("Starting capture loop every %s (camera mode %d)", interval, cameraMode))

	runOnce() // first capture immediately

	for {
		select {
		case <-ticker.C:
			runOnce()
		case sig := <-sigCh:
			logMsg(fmt.Sprintf("Received signal %v, shutting down", sig))
			return
		}
	}
}
