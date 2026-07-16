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
	"path/filepath"
	"strings"
	"time"
)

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
