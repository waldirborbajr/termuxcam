package termuxcam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestChironPostsPromptAndFormatsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chiron" {
			t.Errorf("request = %s %s, want POST /chiron", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var request struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Chiron request: %v", err)
		}
		if request.Text != "What is 2 < 3?" {
			t.Errorf("prompt = %q, want original prompt", request.Text)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"Use <code> safely & clearly","strategy":"gguf","model":"Qwen <0.5B>"}`))
	}))
	defer server.Close()

	response, err := requestChiron(server.URL+"/chiron", "What is 2 < 3?")
	if err != nil {
		t.Fatal(err)
	}
	got := formatChironResponse(response)
	for _, want := range []string{
		"🤖 <b>Chiron</b>",
		"Qwen &lt;0.5B&gt; · gguf",
		"Use &lt;code&gt; safely &amp; clearly",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted response = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "<code> safely") {
		t.Errorf("formatted answer contains unescaped HTML: %q", got)
	}
}

func TestRequestChironRejectsInvalidEndpointAndAPIError(t *testing.T) {
	if _, err := requestChiron("ftp://raspi.local/chiron", "hello"); err == nil {
		t.Fatal("requestChiron() error = nil, want invalid endpoint error")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"answer":"Generation error: model unavailable","strategy":"gguf","model":"Qwen"}`))
	}))
	defer server.Close()

	response, err := requestChiron(server.URL, "hello")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("requestChiron() error = %v, want HTTP 500 error", err)
	}
	if response.Answer != "Generation error: model unavailable" {
		t.Errorf("response answer = %q, want API error details", response.Answer)
	}
}

func TestRasplabCommandFormatsTelemetryAndEscapesHTML(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"rasp<lab>","version":"0.1.0","cpu_temperature_celsius":47.236,"memory_used_mb":742,"memory_total_mb":905,"uptime_seconds":2790654,"ip":"192.168.1.15","hostname":"rasp<lab>"}`))
	}))
	defer server.Close()

	setTestConfig(t, Config{RasplabEndpoint: server.URL})
	got := rasplabCommand()
	for _, want := range []string{
		"📊 <b>rasp&lt;lab&gt; v0.1.0 — telemetry</b>",
		"🌡 CPU temp: <code>47.2 °C</code>",
		"🧠 RAM: <code>742 MB / 905 MB</code>",
		"⏱ Uptime: <code>32d 7h 10m</code>",
		"🌐 IP: <code>192.168.1.15</code>",
		"🖥 Hostname: <code>rasp&lt;lab&gt;</code>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rasplabCommand() = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "<pre>") || strings.Contains(got, `"name"`) {
		t.Errorf("rasplabCommand() = %q, want formatted telemetry instead of raw JSON", got)
	}
}

func TestRasplabCommandRejectsInvalidEndpointAndJSON(t *testing.T) {
	setTestConfig(t, Config{RasplabEndpoint: "http://localhost/rasplab"})
	if got := rasplabCommand(); got != "rasplab endpoint must be a valid HTTPS URL" {
		t.Fatalf("rasplabCommand() = %q, want invalid HTTPS endpoint response", got)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	ConfigMu.Lock()
	AppConfig.RasplabEndpoint = server.URL
	ConfigMu.Unlock()
	if got := rasplabCommand(); !strings.HasPrefix(got, "rasplab API returned invalid JSON:") {
		t.Fatalf("rasplabCommand() = %q, want invalid JSON response", got)
	}
}

func TestCaptureCommandReturnsWhenCaptureIsBusy(t *testing.T) {
	captureMu.Lock()
	defer captureMu.Unlock()

	got := captureCommand([]string{"0", "4"})
	if got != "another capture is already in progress" {
		t.Fatalf("captureCommand() = %q, want busy response", got)
	}
}

func TestLogCommandReturnsEscapedRecentLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "capture.log")
	if err := os.WriteFile(logPath, []byte("previous line\n<unsafe>\nlatest line"), 0600); err != nil {
		t.Fatalf("write test log: %v", err)
	}

	previousLogFile := CLI.LogFile
	CLI.LogFile = logPath
	t.Cleanup(func() { CLI.LogFile = previousLogFile })

	got := logCommand()
	if !strings.Contains(got, "latest line") || !strings.Contains(got, "&lt;unsafe&gt;") {
		t.Fatalf("logCommand() = %q, want recent escaped log contents", got)
	}
}

func TestLogCommandLimitsOutput(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "capture.log")
	contents := strings.Repeat("x", logTailBytes+100)
	if err := os.WriteFile(logPath, []byte(contents), 0600); err != nil {
		t.Fatalf("write test log: %v", err)
	}

	previousLogFile := CLI.LogFile
	CLI.LogFile = logPath
	t.Cleanup(func() { CLI.LogFile = previousLogFile })

	got := logCommand()
	if len(got) > logTailBytes+100 {
		t.Fatalf("logCommand() length = %d, want bounded output", len(got))
	}
	if !strings.Contains(got, "...\n") {
		t.Fatalf("logCommand() = %q, want truncation marker", got)
	}
}
