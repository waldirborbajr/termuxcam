package termuxcam

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	httpAddr        = ":8080"
	httpsAddr       = ":8443"
	rwTimeout       = 30 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 30 * time.Second
)

// forbiddenConfigKeys mirrors the blacklist used by the Telegram /config set
// command. These settings are deliberately device-only.
var forbiddenConfigKeys = []string{"key", "token", "password", "secret"}

// StartAPIServer runs the HTTP/HTTPS API with graceful shutdown. It blocks
// until both servers stop or a signal is received. Call it in a goroutine.
func StartAPIServer(port int) {
	// The `port` argument only controls the HTTP listener for backward
	// compatibility; HTTPS always uses httpsAddr. If port == 0, both are
	// disabled.
	if port == 0 {
		return
	}
	httpListen := fmt.Sprintf(":%d", port)

	cert, err := EnsureSelfSignedCert()
	if err != nil {
		log.Printf("❌ failed to prepare TLS certificate: %v", err)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", handleStatusAPI)
	mux.HandleFunc("/health", handleHealthAPI)
	mux.HandleFunc("/config", handleConfigAPI)
	mux.HandleFunc("/capture", handleCaptureAPI)

	httpsSrv := &http.Server{
		Addr:         httpsAddr,
		Handler:      mux,
		ReadTimeout:  rwTimeout,
		WriteTimeout: rwTimeout,
		IdleTimeout:  idleTimeout,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	// HTTP: /health responds directly (useful for probes without TLS).
	// Everything else redirects to HTTPS.
	httpSrv := &http.Server{
		Addr: httpListen,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/health" {
				writeJSON(w, http.StatusOK, map[string]string{
					"status":  "ok",
					"version": ProjectVersion,
				})
				return
			}
			target := "https://" + r.Host + httpsAddr + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
		ReadTimeout:  rwTimeout,
		WriteTimeout: rwTimeout,
		IdleTimeout:  idleTimeout,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stop)

	errCh := make(chan error, 2)

	go func() {
		log.Printf("🔒 API (HTTPS) listening on https://localhost%s", httpsAddr)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("https server: %w", err)
		}
	}()

	go func() {
		log.Printf("↪️  HTTP → HTTPS redirect on http://localhost%s", httpListen)
		log.Printf("   /health available over plain HTTP (no redirect) for probes.")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	select {
	case <-stop:
		log.Println("🛑 shutdown signal received, starting graceful shutdown...")
	case err := <-errCh:
		log.Printf("❌ server error, starting graceful shutdown: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("⚠️  HTTP shutdown error: %v", err)
	}
	if err := httpsSrv.Shutdown(ctx); err != nil {
		log.Printf("⚠️  HTTPS shutdown error: %v", err)
	}
	log.Println("✅ API servers stopped cleanly")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// /status
// ---------------------------------------------------------------------------

func handleStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"version":  ProjectVersion,
		"telegram": GetStatus(),
		"daemon":   DaemonStatusString(),
	})
}

// ---------------------------------------------------------------------------
// /health
// ---------------------------------------------------------------------------

func handleHealthAPI(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet) {
		return
	}
	config := GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "healthy",
		"version":     ProjectVersion,
		"camera":      "OK",
		"interval":    config.CaptureInterval.String(),
		"camera_mode": config.CameraMode,
		"burst":       config.BurstCount,
		"keep_logs":   config.KeepLogsDays,
		"encryption":  config.EncryptionKey != "",
	})
}

// ---------------------------------------------------------------------------
// /config
// ---------------------------------------------------------------------------

func handleConfigAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, GetConfig())
		return

	case http.MethodPost, http.MethodPut:
		var updates map[string]string
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if len(updates) == 0 {
			http.Error(w, "empty update set", http.StatusBadRequest)
			return
		}

		for k := range updates {
			lower := strings.ToLower(k)
			for _, banned := range forbiddenConfigKeys {
				if strings.Contains(lower, banned) {
					http.Error(w,
						fmt.Sprintf("setting %q can only be changed on the device", k),
						http.StatusForbidden)
					return
				}
			}
		}

		for k, v := range updates {
			if err := SetConfigValue(k + "=" + v); err != nil {
				http.Error(w, fmt.Sprintf("invalid setting %q: %v", k, err), http.StatusBadRequest)
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "updated",
			"config": GetConfig(),
		})
		return

	default:
		w.Header().Set("Allow", "GET, POST, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// /capture
// ---------------------------------------------------------------------------

func handleCaptureAPI(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodPost) {
		return
	}

	camera := -1
	burst := 1

	if v := r.URL.Query().Get("camera"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < -1 || parsed > 1 {
			http.Error(w, "camera must be -1, 0 or 1", http.StatusBadRequest)
			return
		}
		camera = parsed
	}
	if v := r.URL.Query().Get("burst"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 || parsed > 5 {
			http.Error(w, "burst must be between 1 and 5", http.StatusBadRequest)
			return
		}
		burst = parsed
	}

	cameraID := FrontCameraHWID
	label := "Front"
	if camera == 0 {
		cameraID = BackCameraHWID
		label = "Back"
	}

	captureMu.Lock()
	files, captureErr := CaptureBurst(cameraID, burst)
	sent, failed := 0, 0
	for _, f := range files {
		if err := AddExifMetadata(f); err != nil {
			log.Printf("Failed to add metadata: %v", err)
		}
		caption := fmt.Sprintf("📸 %s camera (api)\n🕐 %s",
			label, time.Now().Format("02/01/2006 15:04:05"))

		if Bot != nil {
			if err := Bot.SendPhoto(f, caption); err != nil {
				log.Printf("Photo upload failed: %v", err)
				IncrementFailedUploads()
				failed++
				if pErr := preserveFailedPhoto(f); pErr != nil {
					log.Printf("Failed to preserve photo after upload failure: %v", pErr)
				}
				continue
			}
			IncrementTotalPhotos()
			SetLastCapture(time.Now())
			sent++
		} else {
			failed++
			if pErr := preserveFailedPhoto(f); pErr != nil {
				log.Printf("Failed to preserve photo without Telegram configuration: %v", pErr)
			}
			continue
		}
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove processed photo: %v", err)
		}
	}
	captureMu.Unlock()

	resp := map[string]interface{}{
		"camera": camera,
		"label":  label,
		"burst":  burst,
		"sent":   sent,
		"failed": failed,
	}
	if captureErr != nil {
		resp["error"] = captureErr.Error()
		writeJSON(w, http.StatusInternalServerError, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
