package termuxcam

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSetConfigValueAcceptsBoundaryValues(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		want    Config
	}{
		{
			name:    "minimum camera mode",
			setting: "camera=0",
			want:    Config{CaptureInterval: 5 * time.Minute, CameraMode: 0, BurstCount: 1, KeepLogsDays: 3},
		},
		{
			name:    "maximum camera mode",
			setting: "camera=2",
			want:    Config{CaptureInterval: 5 * time.Minute, CameraMode: 2, BurstCount: 1, KeepLogsDays: 3},
		},
		{
			name:    "minimum capture interval",
			setting: "capture=1m",
			want:    Config{CaptureInterval: time.Minute, CameraMode: 1, BurstCount: 1, KeepLogsDays: 3},
		},
		{
			name:    "maximum capture interval alias",
			setting: "interval=24h",
			want:    Config{CaptureInterval: 24 * time.Hour, CameraMode: 1, BurstCount: 1, KeepLogsDays: 3},
		},
		{
			name:    "minimum burst count",
			setting: "burst=1",
			want:    Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 1, KeepLogsDays: 3},
		},
		{
			name:    "maximum burst count",
			setting: "burst=5",
			want:    Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 5, KeepLogsDays: 3},
		},
		{
			name:    "minimum log retention",
			setting: "logs_days=1",
			want:    Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 1, KeepLogsDays: 1},
		},
		{
			name:    "maximum log retention",
			setting: "logs_days=30",
			want:    Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 1, KeepLogsDays: 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configFile := filepath.Join(t.TempDir(), "termuxcam.toml")
			withConfigFile(t, configFile)
			setTestConfig(t, Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 1, KeepLogsDays: 3})

			if err := SetConfigValue(tt.setting); err != nil {
				t.Fatalf("SetConfigValue(%q) error = %v", tt.setting, err)
			}
			if got := GetConfig(); got != tt.want {
				t.Errorf("config after SetConfigValue() = %+v, want %+v", got, tt.want)
			}
			if err := LoadConfig(configFile); err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if got := GetConfig(); got != tt.want {
				t.Errorf("reloaded config = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSetConfigValueRejectsInvalidValuesWithoutChangingConfig(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "termuxcam.toml")
	withConfigFile(t, configFile)
	want := Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 2, KeepLogsDays: 7}
	setTestConfig(t, want)

	settings := []string{
		"",
		"camera=-1",
		"camera=3",
		"camera=invalid",
		"capture=59s",
		"capture=24h1m",
		"capture=invalid",
		"interval=0m",
		"burst=0",
		"burst=6",
		"burst=invalid",
		"logs_days=0",
		"logs_days=31",
		"logs_days=invalid",
		"unknown=value",
	}

	for _, setting := range settings {
		t.Run(setting, func(t *testing.T) {
			if err := SetConfigValue(setting); err == nil {
				t.Fatalf("SetConfigValue(%q) error = nil, want validation error", setting)
			}
			if got := GetConfig(); got != want {
				t.Errorf("config after rejected setting = %+v, want unchanged %+v", got, want)
			}
		})
	}
}

func withConfigFile(t *testing.T, configFile string) {
	t.Helper()
	previousConfigFile := CLI.ConfigFile
	CLI.ConfigFile = configFile
	t.Cleanup(func() { CLI.ConfigFile = previousConfigFile })
}

func setTestConfig(t *testing.T, config Config) {
	t.Helper()
	ConfigMu.Lock()
	previousConfig := AppConfig
	AppConfig = config
	ConfigMu.Unlock()
	t.Cleanup(func() {
		ConfigMu.Lock()
		AppConfig = previousConfig
		ConfigMu.Unlock()
	})
}