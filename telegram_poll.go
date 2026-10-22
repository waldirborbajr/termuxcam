package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// captureMu serialises captures from the daemon, the HTTP API and Telegram commands,
// so one capture cycle never clears the temporary files of another.
var captureMu sync.Mutex

const pollTimeoutSecs = 30

const helpText = `termuxcam commands:
/status - service status
/system - system information
/version - version
/config - show configuration
/config set key=value - change a setting
/config reload - reload the configuration file
/capture [camera] [burst] - take photos (camera: -1 front, 0 back, 1 front; burst: 1-5)
/torch on|off - flashlight
/help - this message`

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

type tgUpdates struct {
	OK          bool       `json:"ok"`
	Result      []tgUpdate `json:"result"`
	Description string     `json:"description"`
}

// StartTelegramPoller receives commands with long polling (getUpdates). No webhook is needed.
// Only messages from the configured TG_CHAT_ID are accepted.
func StartTelegramPoller() {
	if Bot == nil {
		return
	}
	go pollLoop()
}

func pollLoop() {
	// Skip commands sent while the service was down: start after the newest update.
	var offset int64
	if last, err := getUpdates(-1, 0); err == nil && len(last.Result) > 0 {
		offset = last.Result[len(last.Result)-1].UpdateID + 1
	}
	log.Println("Telegram command polling started")

	for {
		updates, err := getUpdates(offset, pollTimeoutSecs)
		if err != nil {
			log.Printf("telegram polling: %s", redact(err))
			time.Sleep(5 * time.Second)
			continue
		}
		for _, u := range updates.Result {
			offset = u.UpdateID + 1
			handleUpdate(u)
		}
	}
}

func getUpdates(offset int64, timeout int) (*tgUpdates, error) {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=%d&offset=%d",
		Bot.Token, timeout, offset)
	client := &http.Client{Timeout: time.Duration(timeout+10) * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out tgUpdates
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram: %s", out.Description)
	}
	return &out, nil
}

func handleUpdate(u tgUpdate) {
	if u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
		return
	}
	if strconv.FormatInt(u.Message.Chat.ID, 10) != Bot.ChatID {
		log.Printf("ignored message from unauthorized chat %d", u.Message.Chat.ID)
		return
	}

	text := strings.TrimSpace(u.Message.Text)
	log.Printf("telegram command: %s", strings.Fields(text)[0])
	if reply := runCommand(text); reply != "" {
		sendText(reply)
	}
}

func runCommand(text string) string {
	fields := strings.Fields(text)
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if i := strings.Index(cmd, "@"); i >= 0 { // "/help@MyBot" in group chats
		cmd = cmd[:i]
	}
	args := fields[1:]

	switch cmd {
	case "help", "start":
		return helpText
	case "version":
		return "termuxcam v1.0.0\nGo project for camera capture in Termux"
	case "status":
		return fmt.Sprint(GetStatus())
	case "system":
		return systemStatusText()
	case "config":
		return configCommand(args)
	case "capture":
		return captureCommand(args)
	case "torch":
		return torchCommand(args)
	default:
		return "Unknown command. Send /help"
	}
}

func configCommand(args []string) string {
	if len(args) == 0 {
		return fmt.Sprint(GetConfig())
	}
	switch args[0] {
	case "get":
		return fmt.Sprint(GetConfig())
	case "reload":
		if err := LoadConfig(resolveBinaryPath(CLI.ConfigFile)); err != nil {
			return "error: " + err.Error()
		}
		return "configuration reloaded"
	case "set":
		if len(args) < 2 || !strings.Contains(args[1], "=") {
			return "usage: /config set key=value"
		}
		key := strings.ToLower(strings.SplitN(args[1], "=", 2)[0])
		for _, banned := range []string{"key", "token", "password", "secret"} {
			if strings.Contains(key, banned) {
				return "this setting can only be changed on the device"
			}
		}
		if err := SetConfigValue(args[1]); err != nil {
			return "error: " + err.Error()
		}
		return "configuration updated"
	}
	return "usage: /config [get|reload|set key=value]"
}

func captureCommand(args []string) string {
	camera, burst := -1, 1
	if len(args) >= 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < -1 || v > 1 {
			return "camera must be -1, 0 or 1"
		}
		camera = v
	}
	if len(args) >= 2 {
		v, err := strconv.Atoi(args[1])
		if err != nil || v < 1 || v > 5 {
			return "burst must be between 1 and 5"
		}
		burst = v
	}

	cameraID, label := FrontCameraHWID, "Front"
	if camera == 0 {
		cameraID, label = BackCameraHWID, "Back"
	}

	captureMu.Lock()
	defer captureMu.Unlock()

	files, captureErr := CaptureBurst(cameraID, burst)
	sent, failed := 0, 0
	for _, f := range files {
		if err := AddExifMetadata(f); err != nil {
			log.Printf("Failed to add metadata: %v", err)
		}
		caption := fmt.Sprintf("📸 %s camera (command)\n🕐 %s",
			label, time.Now().Format("02/01/2006 15:04:05"))

		if err := Bot.SendPhoto(f, caption); err != nil {
			log.Printf("Photo upload failed: %s", redact(err))
			IncrementFailedUploads()
			failed++
			if pErr := preserveFailedPhoto(f); pErr != nil {
				log.Printf("Failed to preserve photo after upload failure: %v", pErr)
			}
			continue
		}

		IncrementTotalPhotos()
		SetLastCapture(time.Now())
		sent++
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove processed photo: %v", err)
		}
	}

	if captureErr != nil {
		return fmt.Sprintf("capture failed: %v\nsent %d photo(s), %d failed", captureErr, sent, failed)
	}
	return fmt.Sprintf("sent %d photo(s), %d failed", sent, failed)
}

func torchCommand(args []string) string {
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		return "usage: /torch on|off"
	}
	out, err := exec.Command("termux-torch", args[0]).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return "flashlight " + args[0]
}

// sendText sends a plain-text message (no Markdown, so underscores and symbols are safe).
// Telegram limits messages to 4096 characters, so long replies are split.
func sendText(text string) {
	runes := []rune(text)
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", Bot.Token)
	client := &http.Client{Timeout: 10 * time.Second}

	for start := 0; start < len(runes); start += 4000 {
		end := start + 4000
		if end > len(runes) {
			end = len(runes)
		}
		form := url.Values{"chat_id": {Bot.ChatID}, "text": {string(runes[start:end])}}
		resp, err := client.PostForm(endpoint, form)
		if err != nil {
			log.Printf("telegram reply failed: %s", redact(err))
			return
		}
		resp.Body.Close()
	}
}

// systemStatusText formats the data from GetSystemInfo as plain text for Telegram.
func systemStatusText() string {
	info := GetSystemInfo()

	var sb strings.Builder
	fmt.Fprintf(&sb, "📱 System status\n")
	fmt.Fprintf(&sb, "Device: %s\n", info.DeviceModel)
	fmt.Fprintf(&sb, "Android: %s (API %d)\n", info.AndroidVersion, info.APILevel)
	fmt.Fprintf(&sb, "Termux: %s\n", info.TermuxVersion)
	fmt.Fprintf(&sb, "Architecture: %s\n", info.CPUArch)
	fmt.Fprintf(&sb, "CPU cores: %d\n", info.CPUCores)
	fmt.Fprintf(&sb, "Uptime: %s\n", info.Uptime)
	fmt.Fprintf(&sb, "Memory: %s (free: %s)\n", info.MemoryTotal, info.MemoryFree)
	fmt.Fprintf(&sb, "Storage: %s (free: %s)\n", info.StorageTotal, info.StorageFree)
	fmt.Fprintf(&sb, "Battery: %d%% (%s)\n", info.BatteryLevel, info.BatteryStatus)
	sb.WriteString("Cameras:\n")
	if len(info.Cameras) == 0 {
		sb.WriteString("  none detected\n")
	}
	for _, cam := range info.Cameras {
		fmt.Fprintf(&sb, "  - ID %s, facing %s\n", cam.ID, cam.Facing)
	}
	fmt.Fprintf(&sb, "Checked: %s", info.Timestamp.Format("02/01/2006 15:04:05"))
	return sb.String()
}

// redact removes the bot token from errors, because request URLs contain it.
func redact(err error) string {
	return strings.ReplaceAll(err.Error(), Bot.Token, "***")
}
