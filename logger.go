package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// In-memory metrics, surfaced via /status. Written from camera.go
// (capturePhoto), capture.go (runOnce), telegram.go (sendToTelegram); read
// from telegram.go (handleStatus). metricsMutex guards all of it.
var (
	startTime             = time.Now()
	lastSuccessfulCapture time.Time
	capturesToday         int
	totalCaptures         int
	failedUploadsToday    int
	lastError             string
	metricsMutex          sync.RWMutex
	lastMidnight          = time.Now().Truncate(24 * time.Hour)
)

func logMsg(msg string) {
	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] %s", ts, msg)
	fmt.Println(line)

	os.MkdirAll(outputDir, dirMode)
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err == nil {
		f.WriteString(line + "\n")
		f.Close()
	}
}

func getLastLogLine() string {
	data, err := os.ReadFile(logFilePath)
	if err != nil || len(data) == 0 {
		return "No logs yet"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return lines[len(lines)-1]
}

func getLastLogs(n int) string {
	data, err := os.ReadFile(logFilePath)
	if err != nil || len(data) == 0 {
		return "No logs yet"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func startMidnightReset() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		now := time.Now()
		if now.Truncate(24*time.Hour).After(lastMidnight) {
			metricsMutex.Lock()
			capturesToday = 0
			failedUploadsToday = 0
			lastMidnight = now.Truncate(24 * time.Hour)
			metricsMutex.Unlock()
			logMsg("Daily counters reset at midnight")
		}
	}
}
