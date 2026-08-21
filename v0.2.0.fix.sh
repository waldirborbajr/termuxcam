#!/bin/bash
# Script para limpar e recriar todos os arquivos do termuxcam

set -e

echo "🧹 Removendo arquivos antigos..."
rm -f *.go
rm -f termuxcam.conf .env.example
rm -f Makefile Dockerfile docker-compose.yaml
rm -f build-restart.sh termuxcam-ctl Justfile

echo "📁 Criando arquivos do zero..."

# ========== main.go ==========
cat > main.go << 'EOF'
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
EOF

# ========== cli.go ==========
cat > cli.go << 'EOF'
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
EOF

# ========== config.go ==========
cat > config.go << 'EOF'
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	AppConfig Config
	ConfigMu  sync.RWMutex
)

type Config struct {
	CaptureInterval time.Duration
	CameraMode      int
	BurstCount      int
	KeepLogsDays    int
	EncryptionKey   string
}

func LoadConfig(configFile string) error {
	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	AppConfig = Config{
		CaptureInterval: 5 * time.Minute,
		CameraMode:      1,
		BurstCount:      1,
		KeepLogsDays:    3,
	}

	file, err := os.Open(configFile)
	if err != nil {
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "capture":
			if dur, err := time.ParseDuration(value); err == nil {
				if dur >= time.Minute && dur <= 24*time.Hour {
					AppConfig.CaptureInterval = dur
				}
			}
		case "camera":
			var mode int
			if _, err := fmt.Sscanf(value, "%d", &mode); err == nil && mode >= 0 && mode <= 2 {
				AppConfig.CameraMode = mode
			}
		case "burst":
			var count int
			if _, err := fmt.Sscanf(value, "%d", &count); err == nil && count >= 1 && count <= 5 {
				AppConfig.BurstCount = count
			}
		case "logs_days":
			var days int
			if _, err := fmt.Sscanf(value, "%d", &days); err == nil && days >= 1 && days <= 30 {
				AppConfig.KeepLogsDays = days
			}
		case "encryption_key":
			AppConfig.EncryptionKey = value
		}
	}

	return scanner.Err()
}

func GetConfig() Config {
	ConfigMu.RLock()
	defer ConfigMu.RUnlock()
	return AppConfig
}

func SetConfigValue(setting string) error {
	parts := strings.SplitN(setting, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("formato inválido, use: chave=valor")
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	switch key {
	case "camera":
		var mode int
		if _, err := fmt.Sscanf(value, "%d", &mode); err != nil || mode < 0 || mode > 2 {
			return fmt.Errorf("camera deve ser 0, 1 ou 2")
		}
		AppConfig.CameraMode = mode
	case "interval":
		dur, err := time.ParseDuration(value)
		if err != nil || dur < time.Minute || dur > 24*time.Hour {
			return fmt.Errorf("intervalo inválido (1min-24h)")
		}
		AppConfig.CaptureInterval = dur
	case "burst":
		var count int
		if _, err := fmt.Sscanf(value, "%d", &count); err != nil || count < 1 || count > 5 {
			return fmt.Errorf("burst deve ser entre 1 e 5")
		}
		AppConfig.BurstCount = count
	case "logs_days":
		var days int
		if _, err := fmt.Sscanf(value, "%d", &days); err != nil || days < 1 || days > 30 {
			return fmt.Errorf("logs_days deve ser entre 1 e 30")
		}
		AppConfig.KeepLogsDays = days
	default:
		return fmt.Errorf("chave desconhecida: %s", key)
	}

	return SaveConfig()
}

func SaveConfig() error {
	content := fmt.Sprintf(`# Configuração termuxcam
capture=%s
camera=%d
burst=%d
logs_days=%d
`, AppConfig.CaptureInterval, AppConfig.CameraMode, AppConfig.BurstCount, AppConfig.KeepLogsDays)

	if AppConfig.EncryptionKey != "" {
		content += fmt.Sprintf("encryption_key=%s\n", AppConfig.EncryptionKey)
	}

	return os.WriteFile(CLI.ConfigFile, []byte(content), 0644)
}

func LoadEnvFile(envPath string) error {
	file, err := os.Open(envPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		os.Setenv(key, value)
	}

	return scanner.Err()
}
EOF

# ========== capture.go ==========
cat > capture.go << 'EOF'
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	FrontCameraHWID = "1"
	BackCameraHWID  = "0"
)

func CaptureBurst(cameraID string, count int) ([]string, error) {
	var files []string
	tempDir := filepath.Join(GetBinaryDir(), "camera_captures", "temp")
	os.MkdirAll(tempDir, 0755)

	for i := 0; i < count; i++ {
		filename := fmt.Sprintf("%s_%d_%d.jpg",
			time.Now().Format("20060102_150405"),
			i+1,
			time.Now().UnixNano())

		filePath := filepath.Join(tempDir, filename)

		cmd := exec.Command("termux-camera-photo", "-c", cameraID, filePath)
		if err := cmd.Run(); err != nil {
			return files, fmt.Errorf("erro na captura %d: %w", i+1, err)
		}

		if info, err := os.Stat(filePath); err == nil && info.Size() > 0 {
			files = append(files, filePath)
		}

		time.Sleep(500 * time.Millisecond)
	}

	return files, nil
}

func DoCapture() {
	log.Println("📸 Iniciando ciclo de captura...")

	config := GetConfig()

	cameras := []string{}
	switch config.CameraMode {
	case 0:
		cameras = []string{BackCameraHWID}
	case 1:
		cameras = []string{FrontCameraHWID}
	case 2:
		cameras = []string{FrontCameraHWID, BackCameraHWID}
	}

	for _, cameraID := range cameras {
		files, err := CaptureBurst(cameraID, config.BurstCount)
		if err != nil {
			log.Printf("❌ Erro na captura: %v", err)
			IncrementErrorCount()
			continue
		}

		for _, filePath := range files {
			if err := AddExifMetadata(filePath); err != nil {
				log.Printf("⚠️ Erro ao adicionar metadados: %v", err)
			}

			if config.EncryptionKey != "" {
				encrypted, err := EncryptFile(filePath, config.EncryptionKey)
				if err != nil {
					log.Printf("❌ Erro na criptografia: %v", err)
					continue
				}

				encPath := filepath.Join(GetBinaryDir(), "camera_captures", "encrypted",
					filepath.Base(filePath)+".enc")
				if err := os.WriteFile(encPath, []byte(encrypted), 0644); err != nil {
					log.Printf("❌ Erro ao salvar arquivo criptografado: %v", err)
				}
			}

			if Bot != nil {
				caption := fmt.Sprintf("📸 %s Camera\n🕐 %s",
					map[string]string{FrontCameraHWID: "Frontal", BackCameraHWID: "Traseira"}[cameraID],
					time.Now().Format("02/01/2006 15:04:05"))

				if err := Bot.SendPhoto(filePath, caption); err != nil {
					log.Printf("❌ Erro no upload: %v", err)
					IncrementFailedUploads()
				} else {
					IncrementTotalPhotos()
					SetLastCapture(time.Now())
					log.Printf("✅ Foto enviada: %s", filepath.Base(filePath))
				}
			}

			os.Remove(filePath)
		}
	}

	tempDir := filepath.Join(GetBinaryDir(), "camera_captures", "temp")
	os.RemoveAll(tempDir)
	os.MkdirAll(tempDir, 0755)

	RotateLogs()
}

func RunDaemon() {
	config := GetConfig()
	log.Printf("🔄 Iniciando loop de captura (intervalo: %v)", config.CaptureInterval)

	ticker := time.NewTicker(config.CaptureInterval)
	defer ticker.Stop()

	DoCapture()

	for range ticker.C {
		DoCapture()
	}
}

func RunOnce() {
	log.Println("📸 Executando captura única...")
	DoCapture()
	log.Println("✅ Captura concluída")
}
EOF

# ========== crypto.go ==========
cat > crypto.go << 'EOF'
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

func EncryptFile(filePath, key string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("erro ao criar cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("erro ao criar GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
EOF

# ========== telegram.go ==========
cat > telegram.go << 'EOF'
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var Bot *TelegramBot

type TelegramBot struct {
	Token  string
	ChatID string
}

func InitBot() {
	token := os.Getenv("TG_BOT_TOKEN")
	chatID := os.Getenv("TG_CHAT_ID")

	if token != "" && chatID != "" {
		Bot = &TelegramBot{Token: token, ChatID: chatID}
	}
}

func (b *TelegramBot) SendPhoto(filePath, caption string) error {
	var buffer bytes.Buffer
	writer := io.MultiWriter(&buffer)

	boundary := fmt.Sprintf("----WebKitFormBoundary%d", time.Now().UnixNano())

	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"chat_id\"\r\n\r\n%s\r\n", b.ChatID)

	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"caption\"\r\n\r\n%s\r\n", caption)

	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"photo\"; filename=\"%s\"\r\n", filepath.Base(filePath))
	fmt.Fprintf(writer, "Content-Type: image/jpeg\r\n\r\n")

	if _, err := writer.Write(fileData); err != nil {
		return err
	}

	fmt.Fprintf(writer, "\r\n--%s--\r\n", boundary)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", b.Token)
	req, err := http.NewRequest("POST", url, &buffer)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (b *TelegramBot) SendMessage(text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.Token)

	data := map[string]interface{}{
		"chat_id":    b.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
EOF

# ========== api.go ==========
cat > api.go << 'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func StartAPIServer(port int) {
	http.HandleFunc("/status", handleStatusAPI)
	http.HandleFunc("/health", handleHealthAPI)
	http.HandleFunc("/config", handleConfigAPI)
	http.HandleFunc("/capture", handleCaptureAPI)

	log.Printf("🌐 API Server iniciado na porta %d", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handleStatusAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetStatus())
}

func handleHealthAPI(w http.ResponseWriter, r *http.Request) {
	config := GetConfig()
	health := map[string]interface{}{
		"status":      "healthy",
		"camera":      "OK",
		"interval":    config.CaptureInterval.String(),
		"camera_mode": config.CameraMode,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func handleConfigAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" || r.Method == "PUT" {
		var updates map[string]string
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		for k, v := range updates {
			SetConfigValue(k + "=" + v)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetConfig())
}

func handleCaptureAPI(w http.ResponseWriter, r *http.Request) {
	go DoCapture()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "capture triggered"})
}
EOF

# ========== logs.go ==========
cat > logs.go << 'EOF'
package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func RotateLogs() {
	config := GetConfig()
	if config.KeepLogsDays <= 0 {
		config.KeepLogsDays = 3
	}

	cutoff := time.Now().AddDate(0, 0, -config.KeepLogsDays)
	logDir := filepath.Join(GetBinaryDir(), "camera_captures")

	filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".log") && info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err == nil {
				log.Printf("🗑️ Log antigo removido: %s", path)
			}
		}
		return nil
	})
}
EOF

# ========== metadata.go ==========
cat > metadata.go << 'EOF'
package main

import (
	"fmt"
	"os/exec"
	"time"
)

func AddExifMetadata(filePath string) error {
	if _, err := exec.LookPath("exiftool"); err == nil {
		cmd := exec.Command("exiftool",
			"-overwrite_original",
			fmt.Sprintf("-DateTime=%s", time.Now().Format("2006:01:02 15:04:05")),
			fmt.Sprintf("-Description=TermuxCam %s", time.Now().Format("02/01/2006 15:04:05")),
			"-Make=Termux",
			"-Model=Android",
			filePath,
		)
		return cmd.Run()
	}

	if _, err := exec.LookPath("jhead"); err == nil {
		cmd := exec.Command("jhead",
			fmt.Sprintf("-ts%s", time.Now().Format("2006:01:02_15:04:05")),
			filePath,
		)
		return cmd.Run()
	}

	return nil
}
EOF

# ========== state.go ==========
cat > state.go << 'EOF'
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	appState AppState
	stateMu  sync.RWMutex
)

type AppState struct {
	LastCapture   time.Time `json:"last_capture"`
	ErrorCount    int       `json:"error_count"`
	TotalPhotos   int       `json:"total_photos"`
	FailedUploads int       `json:"failed_uploads"`
	StartTime     time.Time `json:"start_time"`
}

func InitState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.StartTime = time.Now()
}

func GetStatus() string {
	stateMu.RLock()
	defer stateMu.RUnlock()

	status := map[string]interface{}{
		"status":         "running",
		"last_capture":   appState.LastCapture.Format(time.RFC3339),
		"total_photos":   appState.TotalPhotos,
		"error_count":    appState.ErrorCount,
		"failed_uploads": appState.FailedUploads,
		"uptime":         time.Since(appState.StartTime).Round(time.Second).String(),
		"config":         GetConfig(),
	}

	data, _ := json.MarshalIndent(status, "", "  ")
	return string(data)
}

func GetBinaryDir() string {
	execPath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(execPath)
}

func IncrementErrorCount() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.ErrorCount++
}

func IncrementFailedUploads() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.FailedUploads++
}

func IncrementTotalPhotos() {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.TotalPhotos++
}

func SetLastCapture(t time.Time) {
	stateMu.Lock()
	defer stateMu.Unlock()
	appState.LastCapture = t
}
EOF

# ========== system.go ==========
cat > system.go << 'EOF'
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type SystemInfo struct {
	DeviceModel    string       `json:"device_model"`
	AndroidVersion string       `json:"android_version"`
	TermuxVersion  string       `json:"termux_version"`
	APILevel       int          `json:"api_level"`
	CPUArch        string       `json:"cpu_arch"`
	CPUCores       int          `json:"cpu_cores"`
	MemoryTotal    string       `json:"memory_total"`
	MemoryFree     string       `json:"memory_free"`
	StorageTotal   string       `json:"storage_total"`
	StorageFree    string       `json:"storage_free"`
	BatteryLevel   int          `json:"battery_level"`
	BatteryStatus  string       `json:"battery_status"`
	Uptime         string       `json:"uptime"`
	Cameras        []CameraInfo `json:"cameras"`
	Timestamp      time.Time    `json:"timestamp"`
}

type CameraInfo struct {
	ID     string `json:"id"`
	Facing string `json:"facing"`
}

func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		Timestamp: time.Now(),
	}

	info.DeviceModel = getDeviceModel()
	info.AndroidVersion = getAndroidVersion()
	info.TermuxVersion = getTermuxVersion()
	info.APILevel = getAPILevel()
	info.CPUArch = getCPUArch()
	info.CPUCores = getCPUCores()
	info.Uptime = getUptime()

	info.MemoryTotal, info.MemoryFree = getMemoryInfo()
	info.StorageTotal, info.StorageFree = getStorageInfo()
	info.BatteryLevel, info.BatteryStatus = getBatteryInfo()
	info.Cameras = getCameraInfo()

	return info
}

func getDeviceModel() string {
	cmd := exec.Command("getprop", "ro.product.model")
	output, err := cmd.Output()
	if err != nil {
		return "Desconhecido"
	}
	return strings.TrimSpace(string(output))
}

func getAndroidVersion() string {
	cmd := exec.Command("getprop", "ro.build.version.release")
	output, err := cmd.Output()
	if err != nil {
		return "Desconhecida"
	}
	return strings.TrimSpace(string(output))
}

func getAPILevel() int {
	cmd := exec.Command("getprop", "ro.build.version.sdk")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	val, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	return val
}

func getTermuxVersion() string {
	cmd := exec.Command("pkg", "list-installed")
	output, err := cmd.Output()
	if err != nil {
		return "Desconhecida"
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "termux/") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return "Desconhecida"
}

func getCPUArch() string {
	return runtime.GOARCH
}

func getCPUCores() int {
	cmd := exec.Command("nproc")
	output, err := cmd.Output()
	if err != nil {
		return runtime.NumCPU()
	}
	cores, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	if cores == 0 {
		return runtime.NumCPU()
	}
	return cores
}

func getUptime() string {
	cmd := exec.Command("uptime")
	output, err := cmd.Output()
	if err != nil {
		return "Desconhecido"
	}
	return strings.TrimSpace(string(output))
}

func getMemoryInfo() (total, free string) {
	cmd := exec.Command("free", "-h")
	output, err := cmd.Output()
	if err != nil {
		return "N/A", "N/A"
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Mem:") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				total = parts[1]
				free = parts[3]
				return
			}
		}
	}
	return "N/A", "N/A"
}

func getStorageInfo() (total, free string) {
	cmd := exec.Command("df", "-h", ".")
	output, err := cmd.Output()
	if err != nil {
		return "N/A", "N/A"
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) >= 2 {
		parts := strings.Fields(lines[1])
		if len(parts) >= 4 {
			total = parts[1]
			free = parts[3]
			return
		}
	}
	return "N/A", "N/A"
}

func getBatteryInfo() (level int, status string) {
	cmd := exec.Command("termux-battery-status")
	output, err := cmd.Output()
	if err != nil {
		return 0, "Desconhecido"
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "\"percentage\"") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				val := strings.TrimSuffix(strings.TrimSpace(parts[1]), ",")
				level, _ = strconv.Atoi(val)
			}
		}
		if strings.Contains(line, "\"status\"") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				status = strings.Trim(strings.TrimSpace(parts[1]), "\",")
			}
		}
	}
	return
}

func getCameraInfo() []CameraInfo {
	var cameras []CameraInfo

	cmd := exec.Command("termux-camera-info")
	output, err := cmd.Output()
	if err != nil {
		return cameras
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "\"id\"") && strings.Contains(line, "\"facing\"") {
			var id, facing string

			idParts := strings.Split(line, "\"id\"")
			if len(idParts) >= 2 {
				idSubParts := strings.Split(idParts[1], ":")
				if len(idSubParts) >= 2 {
					id = strings.Trim(strings.TrimSpace(idSubParts[1]), "\",")
				}
			}

			facingParts := strings.Split(line, "\"facing\"")
			if len(facingParts) >= 2 {
				facingSubParts := strings.Split(facingParts[1], ":")
				if len(facingSubParts) >= 2 {
					facing = strings.Trim(strings.TrimSpace(facingSubParts[1]), "\",")
				}
			}

			if id != "" && facing != "" {
				cameras = append(cameras, CameraInfo{
					ID:     id,
					Facing: facing,
				})
			}
		}
	}

	return cameras
}

func PrintSystemInfo() {
	info := GetSystemInfo()

	fmt.Println("📱 INFORMAÇÕES DO SISTEMA")
	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("📱 Dispositivo:     %s\n", info.DeviceModel)
	fmt.Printf("🤖 Android:        %s (API %d)\n", info.AndroidVersion, info.APILevel)
	fmt.Printf("📦 Termux:         %s\n", info.TermuxVersion)
	fmt.Printf("🖥️  Arquitetura:    %s\n", info.CPUArch)
	fmt.Printf("🧠 Núcleos CPU:    %d\n", info.CPUCores)
	fmt.Printf("🔄 Uptime:         %s\n", info.Uptime)
	fmt.Println()
	fmt.Printf("💾 Memória Total:  %s\n", info.MemoryTotal)
	fmt.Printf("💾 Memória Livre:  %s\n", info.MemoryFree)
	fmt.Println()
	fmt.Printf("📀 Armazenamento:  %s\n", info.StorageTotal)
	fmt.Printf("📀 Livre:         %s\n", info.StorageFree)
	fmt.Println()
	fmt.Printf("🔋 Bateria:        %d%% (%s)\n", info.BatteryLevel, info.BatteryStatus)
	fmt.Println()
	fmt.Println("📷 Câmeras disponíveis:")
	for _, cam := range info.Cameras {
		fmt.Printf("  - ID: %s, Facing: %s\n", cam.ID, cam.Facing)
	}
	fmt.Println(strings.Repeat("=", 40))
}

func GetSystemInfoString() string {
	info := GetSystemInfo()

	var sb strings.Builder
	sb.WriteString("📱 **SISTEMA - termuxcam**\n\n")
	sb.WriteString(fmt.Sprintf("📱 **Dispositivo:** %s\n", info.DeviceModel))
	sb.WriteString(fmt.Sprintf("🤖 **Android:** %s (API %d)\n", info.AndroidVersion, info.APILevel))
	sb.WriteString(fmt.Sprintf("📦 **Termux:** %s\n", info.TermuxVersion))
	sb.WriteString(fmt.Sprintf("🖥️ **Arquitetura:** %s\n", info.CPUArch))
	sb.WriteString(fmt.Sprintf("🧠 **Núcleos CPU:** %d\n", info.CPUCores))
	sb.WriteString(fmt.Sprintf("🔄 **Uptime:** %s\n", info.Uptime))
	sb.WriteString(fmt.Sprintf("💾 **Memória:** %s (livre: %s)\n", info.MemoryTotal, info.MemoryFree))
	sb.WriteString(fmt.Sprintf("📀 **Armazenamento:** %s (livre: %s)\n", info.StorageTotal, info.StorageFree))
	sb.WriteString(fmt.Sprintf("🔋 **Bateria:** %d%% (%s)\n", info.BatteryLevel, info.BatteryStatus))
	sb.WriteString("\n📷 **Câmeras:**\n")
	for _, cam := range info.Cameras {
		sb.WriteString(fmt.Sprintf("  - ID: %s, %s\n", cam.ID, cam.Facing))
	}

	return sb.String()
}
EOF

# ========== termuxcam.conf ==========
cat > termuxcam.conf << 'EOF'
# termuxcam configuration
# Capture interval — 5m, 10m, 1h, etc (1min-24h)
capture=5m

# Camera mode: 0=back, 1=front, 2=both
camera=1

# Burst count: number of photos per cycle (1-5)
burst=1

# Keep logs for N days (1-30)
logs_days=3
EOF

# ========== .env.example ==========
cat > .env.example << 'EOF'
# Termuxcam Environment Variables
# Copie para .env e preencha

TG_BOT_TOKEN=123456789:ABCDEFxxxxxxxxxxxxxxxx
TG_CHAT_ID=782816475

# Encryption Key (opcional, 32 bytes)
ENCRYPTION_KEY=your-32-byte-encryption-key-here

# Debug mode
DEBUG=false
EOF

# ========== Makefile ==========
cat > Makefile << 'EOF'
.PHONY: build build-all clean install-termux test

BINARY_NAME=termuxcam
BUILD_DIR=./build

build:
	go build -o $(BINARY_NAME) .

build-all:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .
	GOOS=android GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-android-arm64 .
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .

clean:
	rm -f $(BINARY_NAME)
	rm -rf $(BUILD_DIR)

install-termux:
	pkg update && pkg upgrade -y
	pkg install golang git termux-api
	go build -o ~/bins/$(BINARY_NAME) .
	cp termuxcam.conf ~/bins/
	cp .env.example ~/bins/.env
	@echo "✅ Instalação concluída!"

test:
	go test -v ./...

help:
	@echo "Comandos:"
	@echo "  make build          - Compilar para arquitetura atual"
	@echo "  make build-all      - Compilar para múltiplas plataformas"
	@echo "  make install-termux - Instalar no Termux"
	@echo "  make clean          - Limpar arquivos compilados"
	@echo "  make test           - Executar testes"
EOF

# ========== build-restart.sh ==========
cat > build-restart.sh << 'EOF'
#!/bin/bash
set -e
echo "🔨 Compilando termuxcam..."
go build -o ~/bins/termuxcam .
echo "♻️ Reiniciando serviço..."
sv restart termuxcam
echo "📋 Status do serviço:"
sv status termuxcam
echo "📄 Últimas linhas do log:"
tail -5 ~/camera_captures/capture.log
EOF
chmod +x build-restart.sh

# ========== termuxcam-ctl ==========
cat > termuxcam-ctl << 'EOF'
#!/bin/bash
SERVICE_NAME="termuxcam"
SERVICE_DIR="$HOME/.termux/service/$SERVICE_NAME"
VAR_SERVICE="$PREFIX/var/service/$SERVICE_NAME"

case "$1" in
    start)
        echo "▶️ Iniciando $SERVICE_NAME..."
        sv up $SERVICE_NAME
        ;;
    stop)
        echo "⏹️ Parando $SERVICE_NAME..."
        sv down $SERVICE_NAME
        ;;
    restart)
        echo "♻️ Reiniciando $SERVICE_NAME..."
        sv restart $SERVICE_NAME
        ;;
    status)
        echo "📊 Status do $SERVICE_NAME:"
        sv status $SERVICE_NAME
        ;;
    logs)
        echo "📄 Logs do $SERVICE_NAME:"
        tail -f ~/camera_captures/capture.log
        ;;
    install)
        echo "📦 Instalando serviço $SERVICE_NAME..."
        mkdir -p $SERVICE_DIR
        mkdir -p $SERVICE_DIR/log
        
        cat > $SERVICE_DIR/run << 'RUNEOF'
#!/data/data/com.termux/files/usr/bin/sh
export HOME=/data/data/com.termux/files/home
export PATH=/data/data/com.termux/files/usr/bin:$PATH
export TG_BOT_TOKEN="${TG_BOT_TOKEN:-}"
export TG_CHAT_ID="${TG_CHAT_ID:-}"
exec /data/data/com.termux/files/home/bins/termuxcam start --daemon
RUNEOF
        chmod +x $SERVICE_DIR/run
        
        ln -sf $SERVICE_DIR $VAR_SERVICE
        sv up $SERVICE_NAME
        echo "✅ Serviço instalado e iniciado!"
        ;;
    uninstall)
        echo "🗑️ Removendo serviço $SERVICE_NAME..."
        sv down $SERVICE_NAME
        rm -f $VAR_SERVICE
        rm -rf $SERVICE_DIR
        echo "✅ Serviço removido!"
        ;;
    *)
        echo "Uso: $0 {start|stop|restart|status|logs|install|uninstall}"
        exit 1
        ;;
esac
EOF
chmod +x termuxcam-ctl

# ========== Justfile ==========
cat > Justfile << 'EOF'
default:
    @just --list

build:
    go build -o termuxcam .

build-all:
    mkdir -p build
    GOOS=linux GOARCH=amd64 go build -o build/termuxcam-linux-amd64 .
    GOOS=linux GOARCH=arm64 go build -o build/termuxcam-linux-arm64 .
    GOOS=android GOARCH=arm64 go build -o build/termuxcam-android-arm64 .
    GOOS=windows GOARCH=amd64 go build -o build/termuxcam-windows-amd64.exe .

clean:
    rm -f termuxcam
    rm -rf build

install-termux:
    pkg update && pkg upgrade -y
    pkg install golang git termux-api
    go build -o ~/bins/termuxcam .
    cp termuxcam.conf ~/bins/
    cp .env.example ~/bins/.env
    @echo "✅ Instalação concluída!"

test:
    go test -v ./...

start:
    ./termuxcam-ctl start

stop:
    ./termuxcam-ctl stop

restart:
    ./termuxcam-ctl restart

status:
    ./termuxcam-ctl status

logs:
    ./termuxcam-ctl logs

install-service:
    ./termuxcam-ctl install

uninstall-service:
    ./termuxcam-ctl uninstall
EOF

# ========== Dockerfile ==========
cat > Dockerfile << 'EOF'
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o termuxcam .

FROM alpine:latest

RUN apk add --no-cache bash curl exiftool

COPY --from=builder /app/termuxcam /usr/local/bin/
COPY termuxcam.conf /etc/termuxcam.conf
COPY .env.example /etc/.env

WORKDIR /data

ENTRYPOINT ["termuxcam"]
CMD ["start", "--daemon"]
EOF

# ========== docker-compose.yaml ==========
cat > docker-compose.yaml << 'EOF'
version: '3.8'

services:
  termuxcam:
    build: .
    container_name: termuxcam
    restart: unless-stopped
    environment:
      - TG_BOT_TOKEN=${TG_BOT_TOKEN}
      - TG_CHAT_ID=${TG_CHAT_ID}
      - ENCRYPTION_KEY=${ENCRYPTION_KEY}
    volumes:
      - ./data:/data/camera_captures
      - ./config:/config
    ports:
      - "8080:8080"
    command: ["start", "--daemon", "--port=8080"]
EOF

# ========== .devcontainer/devcontainer.json ==========
mkdir -p .devcontainer
cat > .devcontainer/devcontainer.json << 'EOF'
{
    "name": "termuxcam Dev Container",
    "image": "mcr.microsoft.com/devcontainers/go:1.21",
    "customizations": {
        "vscode": {
            "extensions": [
                "golang.Go",
                "ms-azuretools.vscode-docker"
            ]
        }
    },
    "postCreateCommand": "go mod download",
    "mounts": [
        "source=${localWorkspaceFolder}/.env,target=/workspace/.env,type=bind"
    ]
}
EOF

# ========== assets/ ==========
mkdir -p assets
touch assets/.keep

echo ""
echo "✅ Todos os arquivos criados com sucesso!"
echo ""
echo "📁 Arquivos Go:"
ls -la *.go 2>/dev/null | wc -l | xargs echo "  Total:"
echo ""
echo "🚀 Para compilar:"
echo "  go mod download"
echo "  make build"
echo ""
echo "📱 Para instalar no Termux:"
echo "  make install-termux"
echo ""
echo "🔄 Para controlar o serviço:"
echo "  ./termuxcam-ctl install"
echo "  ./termuxcam-ctl start"
echo ""
echo "📋 Comandos disponíveis:"
echo "  ./termuxcam --help"
echo "  ./termuxcam system"
echo "  ./termuxcam status"
