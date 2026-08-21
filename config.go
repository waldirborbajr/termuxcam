package main

import (
	"bufio"
	"fmt"
	"os"
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

func LoadConfig(configFile string) error {
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
		return nil
	}
	defer file.Close()

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
			var mode int
			if _, err := fmt.Sscanf(value, "%d", &mode); err == nil && mode >= 0 && mode <= 2 {
				AppConfig.CameraMode = mode
			}
		case "burst":
			var count int
			if _, err := fmt.Sscanf(value, "%d", &count); err == nil && count >= 1 && count <= 5 {
				AppConfig.BurstCount = count
			}
		case "logs_days":
			var days int
			if _, err := fmt.Sscanf(value, "%d", &days); err == nil && days >= 1 && days <= 30 {
				AppConfig.KeepLogsDays = days
			}
		case "encryption_key":
			AppConfig.EncryptionKey = value
		}
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
		return fmt.Errorf("formato inválido, use: chave=valor")
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	switch key {
	case "camera":
		var mode int
		if _, err := fmt.Sscanf(value, "%d", &mode); err != nil || mode < 0 || mode > 2 {
			return fmt.Errorf("camera deve ser 0, 1 ou 2")
		}
		AppConfig.CameraMode = mode
	case "interval":
		dur, err := time.ParseDuration(value)
		if err != nil || dur < time.Minute || dur > 24*time.Hour {
			return fmt.Errorf("intervalo inválido (1min-24h)")
		}
		AppConfig.CaptureInterval = dur
	case "burst":
		var count int
		if _, err := fmt.Sscanf(value, "%d", &count); err != nil || count < 1 || count > 5 {
			return fmt.Errorf("burst deve ser entre 1 e 5")
		}
		AppConfig.BurstCount = count
	case "logs_days":
		var days int
		if _, err := fmt.Sscanf(value, "%d", &days); err != nil || days < 1 || days > 30 {
			return fmt.Errorf("logs_days deve ser entre 1 e 30")
		}
		AppConfig.KeepLogsDays = days
	default:
		return fmt.Errorf("chave desconhecida: %s", key)
	}

	return SaveConfig()
}

func SaveConfig() error {
	content := fmt.Sprintf(`# Configuração termuxcam
capture=%s
camera=%d
burst=%d
logs_days=%d
`, AppConfig.CaptureInterval, AppConfig.CameraMode, AppConfig.BurstCount, AppConfig.KeepLogsDays)

	if AppConfig.EncryptionKey != "" {
		content += fmt.Sprintf("encryption_key=%s\n", AppConfig.EncryptionKey)
	}

	return os.WriteFile(CLI.ConfigFile, []byte(content), 0644)
}

func LoadEnvFile(envPath string) error {
	file, err := os.Open(envPath)
	if err != nil {
		return err
	}
	defer file.Close()

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
		os.Setenv(key, value)
	}

	return scanner.Err()
}
