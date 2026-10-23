package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigReadsTOMLAndUsesDefaultsForInvalidValues(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "termuxcam.toml")
	contents := `capture = "30s"
camera = 2
burst = 4
logs_days = 7
tg_bot_token = "bot-token"
tg_chat_id = "12345"
encryption_key = "0123456789abcdef"
debug = true
`
	if err := os.WriteFile(configFile, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}

	if err := LoadConfig(configFile); err != nil {
		t.Fatal(err)
	}

	config := GetConfig()
	if config.CaptureInterval != 5*time.Minute {
		t.Errorf("capture interval = %s, want default 5m", config.CaptureInterval)
	}
	if config.CameraMode != 2 || config.BurstCount != 4 || config.KeepLogsDays != 7 {
		t.Errorf("unexpected camera/burst/log settings: %+v", config)
	}
	if config.TelegramBotToken != "bot-token" || config.TelegramChatID != "12345" {
		t.Errorf("Telegram credentials were not loaded: %+v", config)
	}
	if config.EncryptionKey != "0123456789abcdef" || !config.Debug {
		t.Errorf("encryption/debug settings were not loaded: %+v", config)
	}
	if strings.Contains(config.String(), "bot-token") || strings.Contains(config.String(), config.EncryptionKey) {
		t.Error("config string exposes secret values")
	}
}

func TestLoadConfigReportsMalformedTOML(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "termuxcam.toml")
	if err := os.WriteFile(configFile, []byte("camera = ["), 0600); err != nil {
		t.Fatal(err)
	}

	if err := LoadConfig(configFile); err == nil {
		t.Fatal("LoadConfig() error = nil, want malformed TOML error")
	}
}

func TestSaveConfigRoundTripsTOML(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "termuxcam.toml")
	previousConfigFile := CLI.ConfigFile
	CLI.ConfigFile = configFile
	t.Cleanup(func() { CLI.ConfigFile = previousConfigFile })

	want := Config{
		CaptureInterval:  10 * time.Minute,
		CameraMode:       2,
		BurstCount:       3,
		KeepLogsDays:     9,
		TelegramBotToken: "bot-token",
		TelegramChatID:   "12345",
		EncryptionKey:    "0123456789abcdef",
		Debug:            true,
	}
	ConfigMu.Lock()
	AppConfig = want
	ConfigMu.Unlock()

	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	if err := LoadConfig(configFile); err != nil {
		t.Fatal(err)
	}

	got := GetConfig()
	if got != want {
		t.Errorf("round trip config = %+v, want %+v", got, want)
	}
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0600 {
		t.Errorf("config file mode = %04o, want 0600", gotMode)
	}
}

func TestResolveBinaryPathUsesExecutableDirectory(t *testing.T) {
	file, err := os.CreateTemp(GetBinaryDir(), "termuxcam-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	configFile := file.Name()
	t.Cleanup(func() { os.Remove(configFile) })
	if _, err := file.WriteString(`capture = "10m"`); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := LoadConfig(filepath.Base(configFile)); err != nil {
		t.Fatal(err)
	}
	if got := GetConfig().CaptureInterval; got != 10*time.Minute {
		t.Errorf("loaded capture interval = %s, want 10m", got)
	}
}
