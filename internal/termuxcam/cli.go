package termuxcam

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/alecthomas/kong"
)

var CLI struct {
	Start   StartCmd   `cmd:"" help:"Start the capture service"`
	Stop    StopCmd    `cmd:"" help:"Stop the capture service"`
	Log     LogCmd     `cmd:"" help:"Follow the capture log"`
	Restart RestartCmd `cmd:"" help:"Restart the capture service"`
	Status  StatusCmd  `cmd:"" help:"Show service status"`
	Run     RunCmd     `cmd:"" hidden:""`
	Config  ConfigCmd  `cmd:"" help:"Manage configuration"`
	Capture CaptureCmd `cmd:"" help:"Capture photos manually"`
	Torch   TorchCmd   `cmd:"" help:"Turn the flashlight on or off"`
	System  SystemCmd  `cmd:"" help:"Show system information"`
	Version VersionCmd `cmd:"" help:"Show version"`

	Debug      bool   `help:"Enable debug mode"`
	LogFile    string `help:"Log file path" default:"camera_captures/capture.log"`
	ConfigFile string `help:"Configuration file path" default:"termuxcam.toml"`
}

// ---------------------------------------------------------------------------
// start
// ---------------------------------------------------------------------------

type StartCmd struct {
	Port int `help:"HTTP API port" default:"8080"`
}

func (s *StartCmd) Run(ctx *kong.Context) error {
	return Daemonize(CLI.LogFile, CLI.ConfigFile, s.Port)
}

// ---------------------------------------------------------------------------
// stop / restart / status
// ---------------------------------------------------------------------------

type StopCmd struct{}

func (s *StopCmd) Run(ctx *kong.Context) error {
	return StopDaemon()
}

type LogCmd struct{}

func (l *LogCmd) Run(ctx *kong.Context) error {
	cmd := exec.Command("tail", "-f", resolveBinaryPath(CLI.LogFile))
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

type RestartCmd struct {
	Port int `help:"HTTP API port" default:"8080"`
}

func (s *RestartCmd) Run(ctx *kong.Context) error {
	if err := StopDaemon(); err != nil {
		return fmt.Errorf("stop daemon before restart: %w", err)
	}
	return Daemonize(CLI.LogFile, CLI.ConfigFile, s.Port)
}

type StatusCmd struct{}

func (s *StatusCmd) Run(ctx *kong.Context) error {
	fmt.Println(DaemonStatusString())
	return nil
}

type RunCmd struct {
	Port int `help:"HTTP API port" default:"8080"`
}

func (r *RunCmd) Run(ctx *kong.Context) error {
	log.Println("Starting termuxcam in foreground...")
	if err := LoadConfig(CLI.ConfigFile); err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	config := GetConfig()
	log.Printf("Config loaded -> interval=%s | cameraMode=%d | burst=%d | config=%s",
		config.CaptureInterval, config.CameraMode, config.BurstCount, resolveBinaryPath(CLI.ConfigFile))

	InitBot()
	StartTelegramPoller()
	if r.Port > 0 {
		go StartAPIServer(r.Port)
	}
	RunDaemon()
	return nil
}

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

type ConfigCmd struct {
	Get    bool   `help:"Show the current configuration"`
	Set    string `help:"Set a configuration value (key=value)"`
	Reload bool   `help:"Reload configuration from file"`
}

func (c *ConfigCmd) Run(ctx *kong.Context) error {
	if c.Reload {
		return LoadConfig(CLI.ConfigFile)
	}
	if c.Set != "" {
		return SetConfigValue(c.Set)
	}
	fmt.Println(GetConfig())
	return nil
}

// ---------------------------------------------------------------------------
// capture
// ---------------------------------------------------------------------------

type CaptureCmd struct {
	Camera int `help:"Camera ID (0=back, 1=front)" default:"-1"`
	Burst  int `help:"Number of photos" default:"1"`
}

func (c *CaptureCmd) Run(ctx *kong.Context) error {
	if c.Camera < -1 || c.Camera > 1 {
		return fmt.Errorf("camera must be -1 (front/default), 0 (back), or 1 (front)")
	}
	if c.Burst < 1 || c.Burst > 5 {
		return fmt.Errorf("burst count must be between 1 and 5")
	}

	cameraID := FrontCameraHWID
	if c.Camera == 0 {
		cameraID = BackCameraHWID
	}

	files, err := CaptureBurst(cameraID, c.Burst)
	if err != nil {
		return fmt.Errorf("capture failed: %w", err)
	}

	fmt.Printf("✅ Captured %d photo(s)\n", len(files))
	for _, f := range files {
		fmt.Printf("  📸 %s\n", f)
	}
	return nil
}

// ---------------------------------------------------------------------------
// torch
// ---------------------------------------------------------------------------

type TorchCmd struct {
	State string `arg:"" enum:"on,off" help:"Flashlight action (on or off)"`
}

func (t *TorchCmd) Run(ctx *kong.Context) error {
	output, err := exec.Command("termux-torch", t.State).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to turn flashlight %s: %w (%s)", t.State, err, strings.TrimSpace(string(output)))
	}
	if t.State == "on" {
		fmt.Println("Flashlight turned on")
	} else {
		fmt.Println("Flashlight turned off")
	}
	return nil
}

// ---------------------------------------------------------------------------
// system / version
// ---------------------------------------------------------------------------

type SystemCmd struct{}

func (s *SystemCmd) Run(ctx *kong.Context) error {
	PrintSystemInfo()
	return nil
}

type VersionCmd struct{}

func (v *VersionCmd) Run(ctx *kong.Context) error {
	fmt.Printf("termuxcam v%s\n", ProjectVersion)
	fmt.Println("Go project for camera capture in Termux")
	return nil
}
