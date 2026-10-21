package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
)

func main() {
	InitState()

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

	err = ctx.Run()
	if err != nil {
		log.Fatalf("Command failed: %v", err)
	}
}
