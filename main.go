package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const Version = "0.2.0"

// Shared file permissions used across camera.go, state.go, config.go, logger.go.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

const uploadTimeout = 30 * time.Second

var (
	exeDir      string
	outputDir   string
	logFilePath string
	statePath   string

	tgBotToken = strings.TrimSpace(os.Getenv("TG_BOT_TOKEN"))
	tgChatID   = strings.TrimSpace(os.Getenv("TG_CHAT_ID"))

	httpClient = &http.Client{
		Timeout: uploadTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	// Cross-goroutine coordination for hot-reload and manual /photo
	// requests. Only main()'s loop ever mutates the ticker or the motion
	// `state` map, so both are funneled through channels rather than
	// touched directly from the Telegram polling goroutine.
	reloadCh chan struct{}
	photoCh  chan struct{}
)

func getExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), "bins")
	}
	return filepath.Dir(exe)
}

func main() {
	if tgBotToken == "" || tgChatID == "" {
		fmt.Println("ERROR: TG_BOT_TOKEN or TG_CHAT_ID not set")
		os.Exit(1)
	}

	os.Setenv("TZ", "America/Sao_Paulo")
	if loc, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		time.Local = loc
	}

	exeDir = getExeDir()
	outputDir = filepath.Join(exeDir, "camera_captures")
	logFilePath = filepath.Join(outputDir, "capture.log")
	statePath = filepath.Join(outputDir, stateFileName)

	os.MkdirAll(outputDir, dirMode)

	reloadCh = make(chan struct{}, 1)
	photoCh = make(chan struct{}, 1)

	loadConfig()
	state := loadState()

	acquireWakeLock()
	defer releaseWakeLock()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// SIGHUP triggers a config reload without restarting the process.
	// loadConfig() itself is safe to call from any goroutine (it commits
	// atomically under cfgMutex); only the resulting ticker interval change
	// needs to be applied from main, hence the channel handoff.
	go func() {
		sigHup := make(chan os.Signal, 1)
		signal.Notify(sigHup, syscall.SIGHUP)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigHup:
				logMsg("SIGHUP received, reloading config")
				loadConfig()
				select {
				case reloadCh <- struct{}{}:
				default:
				}
			}
		}
	}()

	go startTelegramPolling(ctx)
	go startMidnightReset()

	ticker := time.NewTicker(getConfig().Interval)
	defer ticker.Stop()

	logMsg(fmt.Sprintf("termuxcam v%s started successfully", Version))

	state = runOnce(ctx, getConfig(), state, false)
	saveState(state)

	for {
		select {
		case <-ticker.C:
			state = runOnce(ctx, getConfig(), state, false)
			saveState(state)
		case <-photoCh:
			state = runOnce(ctx, getConfig(), state, true)
			saveState(state)
		case <-reloadCh:
			newInterval := getConfig().Interval
			ticker.Reset(newInterval)
			logMsg(fmt.Sprintf("Ticker reset after config reload, new interval=%s", newInterval))
		case <-ctx.Done():
			logMsg("Shutting down on signal")
			return
		}
	}
}
