package termuxcam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
