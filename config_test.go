package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigUsesEnvironmentKeyAndRejectsMalformedNumbers(t *testing.T) {
	const environmentKey = "0123456789abcdef0123456789abcdef"
	t.Setenv("ENCRYPTION_KEY", environmentKey)

	configFile := filepath.Join(t.TempDir(), "termuxcam.conf")
	contents := "camera=2extra\nburst=4extra\nlogs_days=7days\nencryption_key=config-key\n"
	if err := os.WriteFile(configFile, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}

	if err := LoadConfig(configFile); err != nil {
		t.Fatal(err)
	}

	config := GetConfig()
	if config.CameraMode != 1 {
		t.Errorf("camera mode = %d, want default 1", config.CameraMode)
	}
	if config.BurstCount != 1 {
		t.Errorf("burst count = %d, want default 1", config.BurstCount)
	}
	if config.KeepLogsDays != 3 {
		t.Errorf("log retention = %d, want default 3", config.KeepLogsDays)
	}
	if config.EncryptionKey != environmentKey {
		t.Errorf("encryption key = %q, want environment value", config.EncryptionKey)
	}
}
