package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func RotateLogs() {
	config := GetConfig()
	if config.KeepLogsDays <= 0 {
		config.KeepLogsDays = 3
	}

	cutoff := time.Now().AddDate(0, 0, -config.KeepLogsDays)
	logDir := filepath.Join(GetBinaryDir(), "camera_captures")

	filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".log") && info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err == nil {
				log.Printf("🗑️ Log antigo removido: %s", path)
			}
		}
		return nil
	})
}
