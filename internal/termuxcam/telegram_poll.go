package termuxcam

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// captureMu serialises captures from the daemon, the HTTP API and Telegram commands,
// so one capture cycle never clears the temporary files of another.
var captureMu sync.Mutex

const pollTimeoutSecs = 30

const logTailBytes = 600

const rasplabMaxResponseBytes = 3500

const rule = "━━━━━━━━━━━━━━━━━━━━"

const hintFooter = "ℹ️ Send /help for more commands."

const helpText = `<b>termuxcam commands</b>
/status - service status
/system - system information
/version - version
/config - show configuration
/config set key=value - change a setting
/config reload - reload the configuration file
/rasplab - Raspberry Pi information
/capture [camera] [burst] - take photos (camera: -1 front, 0 back, 1 front; burst: 1-5)
/log - send the latest log output
/torch on|off - flashlight
/help - this message`

var uptimeRe = regexp.MustCompile(`up\s+(.*?),\s+\d+\s+users?`)

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
// Only messages from the configured Telegram chat are accepted.
func StartTelegramPoller() {
	if Bot == nil {
		return
	}
	go supervisePollLoop()
}

func supervisePollLoop() {
	for {
		if !runPollLoop() {
			return
		}
		log.Println("Telegram poller stopped after panic; restarting")
		time.Sleep(time.Second)
	}
}

func runPollLoop() (panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("Telegram poller panic: %v", recovered)
			panicked = true
		}
	}()
	pollLoop()
	return false
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

// runCommand returns an HTML message for Telegram.
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
		return fmt.Sprintf("<b>termuxcam</b> v%s\nGo project for camera capture in Termux", ProjectVersion)
	case "status":
		return html.EscapeString(fmt.Sprint(GetStatus())) + "\n\n" + hintFooter
	case "system":
		return systemStatusText()
	case "rasplab":
		return rasplabCommand()
	case "config":
		return configCommand(args)
	case "capture":
		return captureCommand(args)
	case "log":
		return logCommand()
	case "torch":
		return torchCommand(args)
	default:
		return "Unknown command.\n" + hintFooter
	}
}

func rasplabCommand() string {
	endpoint := GetConfig().RasplabEndpoint
	parsedEndpoint, err := url.ParseRequestURI(endpoint)
	if err != nil || parsedEndpoint.Scheme != "https" || parsedEndpoint.Host == "" {
		return "rasplab endpoint must be a valid HTTPS URL"
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "rasplab request failed: " + html.EscapeString(err.Error())
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true,
			},
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "rasplab request failed: " + html.EscapeString(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("rasplab API returned HTTP %d", resp.StatusCode)
	}

	const maxResponseBytes = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "could not read rasplab response: " + html.EscapeString(err.Error())
	}
	if len(body) > maxResponseBytes {
		return "rasplab response is too large"
	}

	var telemetry struct {
		Name                  string   `json:"name"`
		Version               string   `json:"version"`
		CPUTemperatureCelsius *float64 `json:"cpu_temperature_celsius"`
		MemoryUsedMB          *int64   `json:"memory_used_mb"`
		MemoryTotalMB         *int64   `json:"memory_total_mb"`
		UptimeSeconds         *int64   `json:"uptime_seconds"`
	}
	if err := json.Unmarshal(body, &telemetry); err != nil {
		return "rasplab API returned invalid JSON: " + html.EscapeString(err.Error())
	}
	name := html.EscapeString(orUnknown(telemetry.Name))
	version := html.EscapeString(orUnknown(telemetry.Version))
	cpuTemperature := "unknown"
	if telemetry.CPUTemperatureCelsius != nil {
		cpuTemperature = fmt.Sprintf("%.1f °C", *telemetry.CPUTemperatureCelsius)
	}
	memoryUsed := formatRasplabMemory(telemetry.MemoryUsedMB)
	memoryTotal := formatRasplabMemory(telemetry.MemoryTotalMB)
	uptime := formatRasplabUptime(telemetry.UptimeSeconds)

	result := fmt.Sprintf(
		"📊 <b>%s v%s — telemetry</b>\n🌡 CPU temp: <code>%s</code>\n🧠 RAM: <code>%s MB / %s MB</code>\n⏱ Uptime: <code>%s</code>",
		name, version, html.EscapeString(cpuTemperature), html.EscapeString(memoryUsed),
		html.EscapeString(memoryTotal), html.EscapeString(uptime),
	)
	if len(result) > rasplabMaxResponseBytes {
		return "rasplab response is too large to send to Telegram"
	}
	return result
}

func formatRasplabMemory(memoryMB *int64) string {
	if memoryMB == nil || *memoryMB < 0 {
		return "unknown"
	}
	return strconv.FormatInt(*memoryMB, 10)
}

func formatRasplabUptime(uptimeSeconds *int64) string {
	if uptimeSeconds == nil || *uptimeSeconds < 0 {
		return "unknown"
	}

	days := *uptimeSeconds / 86400
	hours := (*uptimeSeconds % 86400) / 3600
	minutes := (*uptimeSeconds % 3600) / 60
	return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}

func configCommand(args []string) string {
	if len(args) == 0 {
		return html.EscapeString(fmt.Sprint(GetConfig()))
	}
	switch args[0] {
	case "get":
		return html.EscapeString(fmt.Sprint(GetConfig()))
	case "reload":
		if err := LoadConfig(CLI.ConfigFile); err != nil {
			return errText(err)
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
			return errText(err)
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

	if !captureMu.TryLock() {
		return "another capture is already in progress"
	}
	go func() {
		defer captureMu.Unlock()
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("Telegram capture panic: %v", recovered)
				sendText("capture failed unexpectedly")
			}
		}()
		sendText(runCapture(cameraID, label, burst))
	}()
	return "capture started; the result will be sent when it finishes"
}

func runCapture(cameraID, label string, burst int) string {
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
		return fmt.Sprintf("capture failed: %s\nsent %d photo(s), %d failed",
			html.EscapeString(captureErr.Error()), sent, failed)
	}
	return fmt.Sprintf("sent %d photo(s), %d failed", sent, failed)
}

func logCommand() string {
	file, err := os.Open(resolveBinaryPath(CLI.LogFile))
	if err != nil {
		return "log file unavailable: " + html.EscapeString(err.Error())
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "cannot read log file: " + html.EscapeString(err.Error())
	}
	if info.Size() > logTailBytes {
		if _, err := file.Seek(-logTailBytes, io.SeekEnd); err != nil {
			return "cannot read log file: " + html.EscapeString(err.Error())
		}
	}
	contents, err := io.ReadAll(io.LimitReader(file, logTailBytes))
	if err != nil {
		return "cannot read log file: " + html.EscapeString(err.Error())
	}
	text := strings.ToValidUTF8(string(contents), "�")
	if info.Size() > logTailBytes {
		text = "...\n" + text
	}
	return "<b>Latest logs</b>\n<pre>" + html.EscapeString(text) + "</pre>"
}

func torchCommand(args []string) string {
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		return "usage: /torch on|off"
	}
	out, err := exec.Command("termux-torch", args[0]).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("failed: %s (%s)",
			html.EscapeString(err.Error()), html.EscapeString(strings.TrimSpace(string(out))))
	}
	return "flashlight " + args[0]
}

// systemStatusText builds the /system message from GetSystemInfo, styled like the noipclient reports.
func systemStatusText() string {
	info := GetSystemInfo()

	termux := orUnknown(info.TermuxVersion)
	if termux == "unknown" {
		termux = orUnknown(os.Getenv("TERMUX_VERSION"))
	}

	var sb strings.Builder
	writeLine := func(line string) {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	writeLine("📱 <b>System status</b>")
	writeLine(rule)

	sb.WriteString("📱 <b>Device</b>\n")
	writeLine(row("Model", orUnknown(info.DeviceModel)))
	writeLine(row("Android", fmt.Sprintf("%s (API %d)", orUnknown(info.AndroidVersion), info.APILevel)))
	writeLine(row("Termux", termux))
	writeLine(row("Architecture", info.CPUArch))
	writeLine(row("CPU cores", strconv.Itoa(info.CPUCores)))
	sb.WriteByte('\n')

	sb.WriteString("⏱ <b>Runtime</b>\n")
	writeLine(row("Uptime", cleanUptime(info.Uptime)))
	writeLine(row("Checked", info.Timestamp.Format("02/01/2006 15:04:05")))
	sb.WriteByte('\n')

	sb.WriteString("💾 <b>Resources</b>\n")
	writeLine(row("Memory", fmt.Sprintf("%s total, %s free", info.MemoryTotal, info.MemoryFree)))
	writeLine(row("Storage", fmt.Sprintf("%s total, %s free", info.StorageTotal, info.StorageFree)))
	writeLine(row("Battery", fmt.Sprintf("%d%% (%s)", info.BatteryLevel, orUnknown(info.BatteryStatus))))
	sb.WriteByte('\n')

	sb.WriteString("📷 <b>Cameras</b>\n")
	if len(info.Cameras) == 0 {
		sb.WriteString("• none detected\n")
	}
	for _, cam := range info.Cameras {
		writeLine(row("ID "+cam.ID, cam.Facing))
	}

	sb.WriteByte('\n')
	sb.WriteString(hintFooter)
	return sb.String()
}

// row renders one "• label: value" line, with the value in monospace.
func row(label, value string) string {
	return fmt.Sprintf("• %s: <code>%s</code>", html.EscapeString(label), html.EscapeString(value))
}

// orUnknown replaces the Portuguese placeholders returned by system.go.
func orUnknown(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "N/A" || strings.HasPrefix(v, "Desconhec") {
		return "unknown"
	}
	return v
}

// cleanUptime keeps only the duration from the `uptime` output.
func cleanUptime(raw string) string {
	if m := uptimeRe.FindStringSubmatch(raw); m != nil {
		return strings.TrimSpace(m[1])
	}
	return orUnknown(raw)
}

// sendText sends an HTML message. Telegram limits messages to 4096 characters, so long replies are split.
func sendText(text string) {
	runes := []rune(text)
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", Bot.Token)
	client := &http.Client{Timeout: 10 * time.Second}

	for start := 0; start < len(runes); start += 4000 {
		end := start + 4000
		if end > len(runes) {
			end = len(runes)
		}
		form := url.Values{
			"chat_id":                  {Bot.ChatID},
			"text":                     {string(runes[start:end])},
			"parse_mode":               {"HTML"},
			"disable_web_page_preview": {"true"},
		}
		resp, err := client.PostForm(endpoint, form)
		if err != nil {
			log.Printf("telegram reply failed: %s", redact(err))
			return
		}
		resp.Body.Close()
	}
}

func errText(err error) string {
	return "error: " + html.EscapeString(err.Error())
}

// redact removes the bot token from errors, because request URLs contain it.
func redact(err error) string {
	return strings.ReplaceAll(err.Error(), Bot.Token, "***")
}
