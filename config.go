package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	AppConfig Config
	ConfigMu  sync.RWMutex
)

type Config struct {
	CaptureInterval time.Duration
	CameraMode      int
	BurstCount      int
	KeepLogsDays    int
	EncryptionKey   string
}

func LoadConfig(configFile string) (returnErr error) {
	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	AppConfig = Config{
		CaptureInterval: 5 * time.Minute,
		CameraMode:      1,
		BurstCount:      1,
		KeepLogsDays:    3,
	}

	file, err := os.Open(configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
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
			if dur, err := time.ParseDuration(value); err == nil {
				if dur >= time.Minute && dur <= 24*time.Hour {
					AppConfig.CaptureInterval = dur
				}
			}
		case "camera":
			mode, err := strconv.Atoi(value)
			if err == nil && mode >= 0 && mode <= 2 {
				AppConfig.CameraMode = mode
			}
		case "burst":
			count, err := strconv.Atoi(value)
			if err == nil && count >= 1 && count <= 5 {
				AppConfig.BurstCount = count
			}
		case "logs_days":
			days, err := strconv.Atoi(value)
			if err == nil && days >= 1 && days <= 30 {
				AppConfig.KeepLogsDays = days
			}
		case "encryption_key":
			AppConfig.EncryptionKey = value
		}
	}
	if key := os.Getenv("ENCRYPTION_KEY"); key != "" {
		AppConfig.EncryptionKey = key
	}

	return scanner.Err()
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

	return SaveConfig()
}

func SaveConfig() error {
	content := fmt.Sprintf(`# termuxcam configuration
capture=%s
camera=%d
burst=%d
logs_days=%d
`, AppConfig.CaptureInterval, AppConfig.CameraMode, AppConfig.BurstCount, AppConfig.KeepLogsDays)

	if AppConfig.EncryptionKey != "" {
		content += fmt.Sprintf("encryption_key=%s\n", AppConfig.EncryptionKey)
	}

	configPath := resolveBinaryPath(CLI.ConfigFile)
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		return err
	}
	return os.Chmod(configPath, 0600)
}

func LoadEnvFile(envPath string) (returnErr error) {
	file, err := os.Open(envPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}
