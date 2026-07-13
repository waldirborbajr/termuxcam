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
	"syscall"
	"time"
)

const (
	cameraID         = "1"
	captureTimeout   = 20 * time.Second
	uploadTimeout    = 30 * time.Second
	intervalDuration = 5 * time.Minute
)

var (
	homeDir, _  = os.UserHomeDir()
	outputDir   = filepath.Join(homeDir, "camera_captures")
	logFilePath = filepath.Join(outputDir, "capture.log")
	tgBotToken  = os.Getenv("TG_BOT_TOKEN")
	tgChatID    = os.Getenv("TG_CHAT_ID")
)

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

func acquireWakeLock() {
	if err := exec.Command("termux-wake-lock").Run(); err != nil {
		logMsg(fmt.Sprintf("failed to acquire wake-lock: %v", err))
		return
	}
	logMsg("wake-lock acquired")
}

func releaseWakeLock() {
	if err := exec.Command("termux-wake-unlock").Run(); err != nil {
		logMsg(fmt.Sprintf("failed to release wake-lock: %v", err))
		return
	}
	logMsg("wake-lock released")
}

func capturePhoto() (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}

	ts := time.Now().Format("20060102_150405")
	outfile := filepath.Join(outputDir, fmt.Sprintf("capture_%s.jpg", ts))

	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "termux-camera-photo", "-c", cameraID, outfile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("capture failed: %s (%v)", stderr.String(), err))
		return "", err
	}

	if _, err := os.Stat(outfile); err != nil {
		logMsg("capture reported success but file not found")
		return "", err
	}

	logMsg(fmt.Sprintf("saved -> %s", outfile))
	return outfile, nil
}

type telegramResponse struct {
	Ok bool `json:"ok"`
}

func sendToTelegram(photoPath string) bool {
	file, err := os.Open(photoPath)
	if err != nil {
		logMsg(fmt.Sprintf("could not open photo: %v", err))
		return false
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writer.WriteField("chat_id", tgChatID); err != nil {
		logMsg(fmt.Sprintf("form field error: %v", err))
		return false
	}

	part, err := writer.CreateFormFile("photo", filepath.Base(photoPath))
	if err != nil {
		logMsg(fmt.Sprintf("form file error: %v", err))
		return false
	}
	if _, err := io.Copy(part, file); err != nil {
		logMsg(fmt.Sprintf("copy error: %v", err))
		return false
	}
	writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", tgBotToken)
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		logMsg(fmt.Sprintf("request build error: %v", err))
		return false
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: uploadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		logMsg(fmt.Sprintf("Telegram send error: %v", err))
		return false
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logMsg(fmt.Sprintf("Telegram send failed: %d %s", resp.StatusCode, string(respBody)))
		return false
	}

	var tgResp telegramResponse
	if err := json.Unmarshal(respBody, &tgResp); err != nil || !tgResp.Ok {
		logMsg(fmt.Sprintf("Telegram response not ok: %s", string(respBody)))
		return false
	}

	logMsg("sent to Telegram")
	return true
}

func runOnce() {
	photo, err := capturePhoto()
	if err != nil {
		return
	}

	if sendToTelegram(photo) {
		if err := os.Remove(photo); err != nil {
			logMsg(fmt.Sprintf("failed to delete local file: %v", err))
		} else {
			logMsg("deleted local file")
		}
	} else {
		logMsg("keeping local file (upload failed)")
	}
}

func main() {
	if tgBotToken == "" || tgChatID == "" {
		logMsg("TG_BOT_TOKEN / TG_CHAT_ID not set — aborting")
		os.Exit(1)
	}

	acquireWakeLock()
	defer releaseWakeLock()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(intervalDuration)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("starting capture loop every %s", intervalDuration))

	runOnce() // capture immediately on start, then wait for ticker

	for {
		select {
		case <-ticker.C:
			runOnce()
		case sig := <-sigCh:
			logMsg(fmt.Sprintf("received signal %v, shutting down", sig))
			return
		}
	}
}
