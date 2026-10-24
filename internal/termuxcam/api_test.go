package termuxcam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleHealthAPI(t *testing.T) {
	setAPIConfigForTest(t, Config{
		CaptureInterval: 10 * time.Minute,
		CameraMode:      2,
		BurstCount:      3,
		KeepLogsDays:    8,
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	handleHealthAPI(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["status"] != "healthy" || response["version"] != ProjectVersion {
		t.Errorf("status/version = %v/%v, want healthy/%s", response["status"], response["version"], ProjectVersion)
	}
	if response["interval"] != "10m0s" || response["camera_mode"] != float64(2) || response["burst"] != float64(3) || response["keep_logs"] != float64(8) {
		t.Errorf("health config fields = %v, want interval 10m0s, camera 2, burst 3, keep_logs 8", response)
	}
}

func TestHandleStatusAPIResponseAndMethod(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	handleStatusAPI(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["status"] != "ok" || response["version"] != ProjectVersion {
		t.Errorf("status/version = %v/%v, want ok/%s", response["status"], response["version"], ProjectVersion)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/status", nil)
	handleStatusAPI(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got := recorder.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want %q", got, http.MethodGet)
	}
}

func TestHandleConfigAPIGetHidesSecrets(t *testing.T) {
	setAPIConfigForTest(t, Config{
		CaptureInterval:  5 * time.Minute,
		CameraMode:       1,
		BurstCount:       1,
		KeepLogsDays:     3,
		TelegramBotToken: "bot-secret",
		TelegramChatID:   "chat-secret",
		EncryptionKey:    "encryption-secret",
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/config", nil)
	handleConfigAPI(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET config = %d, want %d", recorder.Code, http.StatusOK)
	}
	for _, secret := range []string{"bot-secret", "chat-secret", "encryption-secret"} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Errorf("GET config response exposes %q", secret)
		}
	}
}

func TestHandleConfigAPIUpdatePersistsConfig(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "termuxcam.toml")
	withAPIConfigFile(t, configFile)
	setAPIConfigForTest(t, Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 1, KeepLogsDays: 3})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/config", strings.NewReader(`{"burst":"4"}`))
	handleConfigAPI(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST config = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := GetConfig().BurstCount; got != 4 {
		t.Errorf("burst count = %d, want 4", got)
	}
	if err := LoadConfig(configFile); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := GetConfig().BurstCount; got != 4 {
		t.Errorf("persisted burst count = %d, want 4", got)
	}
}

func TestHandleConfigAPIRejectsInvalidAndSensitiveUpdates(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "termuxcam.toml")
	withAPIConfigFile(t, configFile)
	want := Config{CaptureInterval: 5 * time.Minute, CameraMode: 1, BurstCount: 2, KeepLogsDays: 7}
	setAPIConfigForTest(t, want)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "malformed JSON", body: `{"burst":`, wantStatus: http.StatusBadRequest},
		{name: "empty update", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "invalid value", body: `{"burst":"6"}`, wantStatus: http.StatusBadRequest},
		{name: "sensitive key", body: `{"tg_bot_token":"new-secret"}`, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/config", strings.NewReader(tt.body))
			handleConfigAPI(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if got := GetConfig(); got != want {
				t.Errorf("config after rejected update = %+v, want unchanged %+v", got, want)
			}
		})
	}
}

func TestHandleCaptureAPIRejectsInvalidQueries(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "invalid camera", query: "camera=2"},
		{name: "malformed camera", query: "camera=invalid"},
		{name: "invalid burst", query: "burst=6"},
		{name: "malformed burst", query: "burst=invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/capture?"+tt.query, nil)
			handleCaptureAPI(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func withAPIConfigFile(t *testing.T, configFile string) {
	t.Helper()
	previousConfigFile := CLI.ConfigFile
	CLI.ConfigFile = configFile
	t.Cleanup(func() { CLI.ConfigFile = previousConfigFile })
}

func setAPIConfigForTest(t *testing.T, config Config) {
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
