package termuxcam

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

var (
	AppConfig Config
	ConfigMu  sync.RWMutex
)

type Config struct {
	CaptureInterval  time.Duration `toml:"capture"`
	CameraMode       int           `toml:"camera"`
	BurstCount       int           `toml:"burst"`
	KeepLogsDays     int           `toml:"logs_days"`
	TelegramBotToken string        `toml:"tg_bot_token" json:"-"`
	TelegramChatID   string        `toml:"tg_chat_id" json:"-"`
	EncryptionKey    string        `toml:"encryption_key" json:"-"`
	Debug            bool          `toml:"debug"`
}

func (c Config) String() string {
	return fmt.Sprintf(
		"{CaptureInterval:%s CameraMode:%d BurstCount:%d KeepLogsDays:%d TelegramConfigured:%t EncryptionEnabled:%t Debug:%t}",
		c.CaptureInterval,
		c.CameraMode,
		c.BurstCount,
		c.KeepLogsDays,
		c.TelegramBotToken != "" && c.TelegramChatID != "",
		c.EncryptionKey != "",
		c.Debug,
	)
}

func LoadConfig(configFile string) error {
	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	configFile = resolveBinaryPath(configFile)
	AppConfig = Config{
		CaptureInterval: 5 * time.Minute,
		CameraMode:      1,
		BurstCount:      1,
		KeepLogsDays:    3,
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	parsed := struct {
		Capture          string `toml:"capture"`
		Camera           int    `toml:"camera"`
		Burst            int    `toml:"burst"`
		LogsDays         int    `toml:"logs_days"`
		TelegramBotToken string `toml:"tg_bot_token"`
		TelegramChatID   string `toml:"tg_chat_id"`
		EncryptionKey    string `toml:"encryption_key"`
		Debug            bool   `toml:"debug"`
	}{
		Capture:  "5m",
		Camera:   1,
		Burst:    1,
		LogsDays: 3,
	}

	if err := toml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parse TOML configuration %q: %w", configFile, err)
	}
	if dur, err := time.ParseDuration(parsed.Capture); err == nil && dur >= time.Minute && dur <= 24*time.Hour {
		AppConfig.CaptureInterval = dur
	}
	if parsed.Camera >= 0 && parsed.Camera <= 2 {
		AppConfig.CameraMode = parsed.Camera
	}
	if parsed.Burst >= 1 && parsed.Burst <= 5 {
		AppConfig.BurstCount = parsed.Burst
	}
	if parsed.LogsDays >= 1 && parsed.LogsDays <= 30 {
		AppConfig.KeepLogsDays = parsed.LogsDays
	}
	AppConfig.TelegramBotToken = parsed.TelegramBotToken
	AppConfig.TelegramChatID = parsed.TelegramChatID
	AppConfig.EncryptionKey = parsed.EncryptionKey
	AppConfig.Debug = parsed.Debug
	return nil
}

func GetConfig() Config {
	ConfigMu.RLock()
	defer ConfigMu.RUnlock()
	return AppConfig
}

func SetConfigValue(setting string) error {
	parts := strings.SplitN(setting, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid format; use key=value")
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	switch key {
	case "camera":
		mode, err := strconv.Atoi(value)
		if err != nil || mode < 0 || mode > 2 {
			return fmt.Errorf("camera must be 0, 1, or 2")
		}
		AppConfig.CameraMode = mode
	case "capture", "interval":
		dur, err := time.ParseDuration(value)
		if err != nil || dur < time.Minute || dur > 24*time.Hour {
			return fmt.Errorf("invalid interval (1m-24h)")
		}
		AppConfig.CaptureInterval = dur
	case "burst":
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 || count > 5 {
			return fmt.Errorf("burst must be between 1 and 5")
		}
		AppConfig.BurstCount = count
	case "logs_days":
		days, err := strconv.Atoi(value)
		if err != nil || days < 1 || days > 30 {
			return fmt.Errorf("logs_days must be between 1 and 30")
		}
		AppConfig.KeepLogsDays = days
	default:
		return fmt.Errorf("unknown setting: %s", key)
	}

	return saveConfig(AppConfig)
}

func SaveConfig() error {
	return saveConfig(GetConfig())
}

func saveConfig(config Config) error {
	serialized := struct {
		Capture          string `toml:"capture"`
		Camera           int    `toml:"camera"`
		Burst            int    `toml:"burst"`
		LogsDays         int    `toml:"logs_days"`
		TelegramBotToken string `toml:"tg_bot_token"`
		TelegramChatID   string `toml:"tg_chat_id"`
		EncryptionKey    string `toml:"encryption_key"`
		Debug            bool   `toml:"debug"`
	}{
		Capture:          config.CaptureInterval.String(),
		Camera:           config.CameraMode,
		Burst:            config.BurstCount,
		LogsDays:         config.KeepLogsDays,
		TelegramBotToken: config.TelegramBotToken,
		TelegramChatID:   config.TelegramChatID,
		EncryptionKey:    config.EncryptionKey,
		Debug:            config.Debug,
	}
	content, err := toml.Marshal(serialized)
	if err != nil {
		return fmt.Errorf("encode TOML configuration: %w", err)
	}

	configPath := resolveBinaryPath(CLI.ConfigFile)
	if err := os.WriteFile(configPath, content, 0600); err != nil {
		return fmt.Errorf("write TOML configuration %q: %w", configPath, err)
	}
	return os.Chmod(configPath, 0600)
}
