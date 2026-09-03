package main

import (
	"fmt"
	"log"

	"github.com/alecthomas/kong"
)

var CLI struct {
	Start   StartCmd   `cmd:"" help:"Iniciar o serviço de captura"`
	Status  StatusCmd  `cmd:"" help:"Exibir status do serviço"`
	Config  ConfigCmd  `cmd:"" help:"Gerenciar configuração"`
	Capture CaptureCmd `cmd:"" help:"Executar captura manual"`
	System  SystemCmd  `cmd:"" help:"Exibir informações do sistema"`
	Version VersionCmd `cmd:"" help:"Exibir versão"`

	Debug      bool   `help:"Ativar modo debug"`
	LogFile    string `help:"Arquivo de log" default:"camera_captures/capture.log"`
	ConfigFile string `help:"Arquivo de configuração" default:"termuxcam.conf"`
	EnvFile    string `help:"Arquivo .env" default:".env"`
}

type StartCmd struct {
	Daemon bool `help:"Executar como daemon (serviço)"`
	Port   int  `help:"Porta para API HTTP" default:"8080"`
}

func (s *StartCmd) Run(ctx *kong.Context) error {
	log.Println("🚀 Iniciando termuxcam...")

	if err := LoadEnvFile(CLI.EnvFile); err != nil {
		log.Printf("⚠️ Erro ao carregar .env: %v", err)
	}

	if err := LoadConfig(CLI.ConfigFile); err != nil {
		log.Printf("⚠️ Erro ao carregar configuração: %v", err)
	}

	InitBot()

	if s.Port > 0 {
		go StartAPIServer(s.Port)
	}

	if s.Daemon {
		RunDaemon()
	} else {
		RunOnce()
	}

	return nil
}

type StatusCmd struct{}

func (s *StatusCmd) Run(ctx *kong.Context) error {
	status := GetStatus()
	fmt.Println(status)
	return nil
}

type ConfigCmd struct {
	Get    bool   `help:"Exibir configuração atual"`
	Set    string `help:"Definir configuração (formato: chave=valor)"`
	Reload bool   `help:"Recarregar configuração do arquivo"`
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

type CaptureCmd struct {
	Camera int `help:"ID da câmera (0=traseira, 1=frontal)" default:"-1"`
	Burst  int `help:"Número de fotos" default:"1"`
}

func (c *CaptureCmd) Run(ctx *kong.Context) error {
	cameraID := FrontCameraHWID
	if c.Camera == 0 {
		cameraID = BackCameraHWID
	}

	files, err := CaptureBurst(cameraID, c.Burst)
	if err != nil {
		return fmt.Errorf("erro na captura: %w", err)
	}

	fmt.Printf("✅ Capturadas %d foto(s)\n", len(files))
	for _, f := range files {
		fmt.Printf("  📸 %s\n", f)
	}
	return nil
}

type SystemCmd struct{}

func (s *SystemCmd) Run(ctx *kong.Context) error {
	PrintSystemInfo()
	return nil
}

type VersionCmd struct{}

func (v *VersionCmd) Run(ctx *kong.Context) error {
	fmt.Println("termuxcam v1.0.0")
	fmt.Println("GO project to access frontal camera from termux")
	return nil
}
