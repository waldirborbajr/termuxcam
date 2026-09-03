package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
)

func main() {
	InitState()

	logFile := filepath.Join(GetBinaryDir(), "camera_captures", "capture.log")
	os.MkdirAll(filepath.Dir(logFile), 0755)

	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		defer f.Close()
		log.SetOutput(f)
	}

	ctx := kong.Parse(&CLI,
		kong.Name("termuxcam"),
		kong.Description("GO project to access frontal camera from termux"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)

	err = ctx.Run()
	if err != nil {
		log.Fatalf("❌ Erro: %v", err)
	}
}
