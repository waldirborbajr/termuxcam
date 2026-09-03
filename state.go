package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	appState AppState
	stateMu  sync.RWMutex
)

type AppState struct {
	LastCapture   time.Time `json:"last_capture"`
	ErrorCount    int       `json:"error_count"`
	TotalPhotos   int       `json:"total_photos"`
	FailedUploads int       `json:"failed_uploads"`
	StartTime     time.Time `json:"start_time"`
}

func InitState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.StartTime = time.Now()
}

func GetStatus() string {
	stateMu.RLock()
	defer stateMu.RUnlock()

	status := map[string]interface{}{
		"status":         "running",
		"last_capture":   appState.LastCapture.Format(time.RFC3339),
		"total_photos":   appState.TotalPhotos,
		"error_count":    appState.ErrorCount,
		"failed_uploads": appState.FailedUploads,
		"uptime":         time.Since(appState.StartTime).Round(time.Second).String(),
		"config":         GetConfig(),
	}

	data, _ := json.MarshalIndent(status, "", "  ")
	return string(data)
}

func GetBinaryDir() string {
	execPath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(execPath)
}

func IncrementErrorCount() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.ErrorCount++
}

func IncrementFailedUploads() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.FailedUploads++
}

func IncrementTotalPhotos() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.TotalPhotos++
}

func SetLastCapture(t time.Time) {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.LastCapture = t
}
