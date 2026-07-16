package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	configFileName = "termuxcam.conf"
	configFileMode = 0o600

	minInterval = 1 * time.Minute
	maxInterval = 24 * time.Hour

	defaultMotionEnabled   = true
	defaultMotionThreshold = 6
	defaultHeartbeat       = 0
)

// --- Physical hardware camera IDs ------------------------------------------
//
// What `termux-camera-photo -c <id>` actually expects. This is a property of
// the DEVICE, not of this app's config — confirm with `termux-camera-info`
// and update if different on your phone. Nothing else in the codebase ever
// touches a raw ID directly; everything works off the semantic "front"/
// "back" labels via camSpec/camerasForMode below.
const (
	frontCameraHWID = "1"
	backCameraHWID  = "0"
)

// appConfig holds every value that can be set from termuxcam.conf. Reads
// happen from the Telegram polling goroutine (/status, /config) and the main
// loop; writes happen from loadConfig(), which may run on a SIGHUP-handling
// goroutine. cfgMutex makes that safe.
type appConfig struct {
	Interval        time.Duration
	CameraMode      int // 0=back, 1=front, 2=both
	MotionEnabled   bool
	MotionThreshold int
	Heartbeat       time.Duration
}

var (
	cfg = appConfig{
		Interval:        5 * time.Minute,
		CameraMode:      1,
		MotionEnabled:   defaultMotionEnabled,
		MotionThreshold: defaultMotionThreshold,
		Heartbeat:       defaultHeartbeat,
	}
	cfgMutex sync.RWMutex
)

func getConfig() appConfig {
	cfgMutex.RLock()
	defer cfgMutex.RUnlock()
	return cfg
}

type camSpec struct {
	label string // "front" / "back" — used in filenames, captions, logs
	hwID  string // passed verbatim to `termux-camera-photo -c`
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

	// Parse into a local copy first (seeded with current values), then
	// commit atomically — a reload with a malformed line should never leave
	// the live config half-updated.
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
