package termuxcam

import (
	"log"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
)

func Run() {
	InitState()

	// Fast-path for the daemon child: if the environment marker is set, we are
	// the detached process spawned by Daemonize(). Skip the full CLI parsing,
	// wire up logging, and run the service loop.
	if os.Getenv(daemonEnvMarker) == "1" {
		runDaemonChild()
		return
	}

	// Normal path: parse the full CLI.
	ctx := kong.Parse(&CLI,
		kong.Name("termuxcam"),
		kong.Description("Go project to access the front camera from Termux"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)

	logFile := resolveBinaryPath(CLI.LogFile)
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		log.Fatalf("create log directory: %v", err)
	}

	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		defer func() {
			if err := f.Close(); err != nil {
				log.Printf("close log file: %v", err)
			}
		}()
		log.SetOutput(f)
	} else {
		log.Printf("open log file: %v", err)
	}

	if err := ctx.Run(); err != nil {
		log.Fatalf("Command failed: %v", err)
	}
}

// runDaemonChild parses only the flags that Daemonize() passes to the child
// process, redirects logging, and runs the service loop.
func runDaemonChild() {
	var child struct {
		Start      struct{} `cmd:""`
		Port       int      `help:"port" default:"8080"`
		LogFile    string   `help:"log"`
		ConfigFile string   `help:"config"`
	}

	kong.Parse(&child,
		kong.Name("termuxcam-daemon"),
		kong.Exit(func(int) {}), // never terminate on parse error
	)

	logFile := resolveBinaryPath(child.LogFile)
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		log.Fatalf("create log directory: %v", err)
	}
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		defer func() {
			if err := f.Close(); err != nil {
				log.Printf("close log file: %v", err)
			}
		}()
		log.SetOutput(f)
	} else {
		log.Printf("open log file: %v", err)
	}

	RunDaemonChild(child.Port, child.ConfigFile)
}
