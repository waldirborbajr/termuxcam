package termuxcam

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const daemonEnvMarker = "_TERMUXCAM_DAEMON"

// pidFilePath returns the location of the PID file next to the log file.
func pidFilePath() string {
	return filepath.Join(GetBinaryDir(), "camera_captures", "termuxcam.pid")
}

func readPid() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// Daemonize re-executes the current binary in detached mode, redirects the
// output to the log file, writes the PID file, then returns to the caller
// (which should exit). The child runs the same `start` command, but detects
// the environment marker and runs the service loop instead of re-daemonizing.
func Daemonize(logFile, configFile string, port int) error {
	// Refuse to start twice.
	if pid, err := readPid(); err == nil && processAlive(pid) {
		return fmt.Errorf("already running (pid %d)", pid)
	}
	os.Remove(pidFilePath())

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	logPath := resolveBinaryPath(logFile)
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	logFd, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	args := []string{
		"start", "--daemon",
		"--port", strconv.Itoa(port),
		"--log-file", logFile,
		"--config-file", configFile,
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), daemonEnvMarker+"=1")
	cmd.Stdin = nil
	cmd.Stdout = logFd
	cmd.Stderr = logFd
	configureDaemonProcess(cmd)

	if err := cmd.Start(); err != nil {
		logFd.Close()
		return fmt.Errorf("start daemon: %w", err)
	}

	// Write the PID of the detached child.
	pidDir := filepath.Dir(pidFilePath())
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		logFd.Close()
		return fmt.Errorf("create pid directory: %w", err)
	}
	if err := os.WriteFile(pidFilePath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		logFd.Close()
		return fmt.Errorf("write pid file: %w", err)
	}

	logFd.Close()

	// Give the child a moment to fail fast if something is broken.
	time.Sleep(500 * time.Millisecond)
	if !processAlive(cmd.Process.Pid) {
		os.Remove(pidFilePath())
		return errors.New("daemon exited right after start, check the log file")
	}

	fmt.Printf("daemon started (pid %d)\n", cmd.Process.Pid)
	fmt.Printf("log: %s\n", logPath)
	return nil
}

// RunDaemonChild is called from main() when the daemon environment marker is set.
// It skips daemonization and runs the service loop in the current process.
func RunDaemonChild(port int, configFile string) {
	log.Println("termuxcam daemon starting")

	CLI.ConfigFile = configFile
	if err := LoadConfig(configFile); err != nil {
		log.Printf("Failed to load configuration: %v", err)
		return
	}

	InitBot()
	StartTelegramPoller()

	if port > 0 {
		go StartAPIServer(port)
	}

	RunDaemon() // blocks forever
}

// StopDaemon sends SIGTERM to the PID from the pid file, waits up to 5s,
// then SIGKILLs if still alive. Removes the pid file.
func StopDaemon() error {
	pid, err := readPid()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("not running")
			return nil
		}
		return err
	}

	if !processAlive(pid) {
		os.Remove(pidFilePath())
		fmt.Println("not running")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal process: %w", err)
	}

	for i := 0; i < 50; i++ {
		if !processAlive(pid) {
			os.Remove(pidFilePath())
			fmt.Printf("stopped (pid %d)\n", pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = proc.Signal(syscall.SIGKILL)
	os.Remove(pidFilePath())
	fmt.Printf("killed (pid %d)\n", pid)
	return nil
}

// DaemonStatusString returns a human readable status of the background daemon.
func DaemonStatusString() string {
	pid, err := readPid()
	if err != nil {
		return "daemon: not running"
	}
	if processAlive(pid) {
		return fmt.Sprintf("daemon: running (pid %d)", pid)
	}
	return "daemon: not running (stale pid file)"
}
