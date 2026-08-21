package main

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
)

// Constantes de hardware da câmera (ajuste conforme seu dispositivo)
const (
	frontCameraHWID = "1"
	backCameraHWID  = "0"
)

// ========== ESTRUTURA PRINCIPAL COM KONG ==========

// CLI - Estrutura principal usando Kong para parsing de linha de comando
type CLI struct {
	// Comandos principais
	Start   StartCmd   `cmd:"" help:"Iniciar o serviço de captura"`
	Status  StatusCmd  `cmd:"" help:"Exibir status do serviço"`
	Config  ConfigCmd  `cmd:"" help:"Gerenciar configuração"`
	Capture CaptureCmd `cmd:"" help:"Executar captura manual"`
	Version VersionCmd `cmd:"" help:"Exibir versão"`

	// Flags globais
	Debug    bool   `help:"Ativar modo debug"`
	LogFile  string `help:"Arquivo de log" default:"camera_captures/capture.log"`
	ConfigFile string `help:"Arquivo de configuração" default:"termuxcam.conf"`
	EnvFile  string `help:"Arquivo .env" default:".env"`
}

// ========== COMANDO: START ==========

type StartCmd struct {
	// Flags específicas do start
	Daemon bool `help:"Executar como daemon (serviço)"`
	Port   int  `help:"Porta para API HTTP" default:"8080"`
}

func (s *StartCmd) Run(ctx *kong.Context) error {
	log.Println("🚀 Iniciando termuxcam...")
	
	// Carregar .env
	if err := loadEnvFile(cli.EnvFile); err != nil {
		log.Printf("⚠️ Erro ao carregar .env: %v", err)
	}
	
	// Carregar configuração
	if err := loadConfig(cli.ConfigFile); err != nil {
		log.Printf("⚠️ Erro ao carregar configuração: %v", err)
	}
	
	// Inicializar bot
	initBot()
	
	// Iniciar API server (usando stdlib)
	if s.Port > 0 {
		go startAPIServer(s.Port)
	}
	
	// Iniciar loop de captura
	if s.Daemon {
		runDaemon()
	} else {
		runOnce()
	}
	
	return nil
}

// ========== COMANDO: STATUS ==========

type StatusCmd struct{}

func (s *StatusCmd) Run(ctx *kong.Context) error {
	status := getStatus()
	jsonData, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(jsonData))
	return nil
}

// ========== COMANDO: CONFIG ==========

type ConfigCmd struct {
	Get    bool   `help:"Exibir configuração atual"`
	Set    string `help:"Definir configuração (formato: chave=valor)"`
	Reload bool   `help:"Recarregar configuração do arquivo"`
}

func (c *ConfigCmd) Run(ctx *kong.Context) error {
	if c.Reload {
		return loadConfig(cli.ConfigFile)
	}
	
	if c.Set != "" {
		return setConfigValue(c.Set)
	}
	
	// Exibir configuração atual
	config := getConfig()
	jsonData, _ := json.MarshalIndent(config, "", "  ")
	fmt.Println(string(jsonData))
	return nil
}

// ========== COMANDO: CAPTURE ==========

type CaptureCmd struct {
	Camera int `help:"ID da câmera (0=traseira, 1=frontal)" default:"-1"`
	Burst  int `help:"Número de fotos" default:"1"`
}

func (c *CaptureCmd) Run(ctx *kong.Context) error {
	cameraID := frontCameraHWID
	if c.Camera == 0 {
		cameraID = backCameraHWID
	}
	
	files, err := captureBurst(cameraID, c.Burst)
	if err != nil {
		return fmt.Errorf("erro na captura: %w", err)
	}
	
	fmt.Printf("✅ Capturadas %d foto(s)\n", len(files))
	for _, f := range files {
		fmt.Printf("  📸 %s\n", f)
	}
	
	return nil
}

// ========== COMANDO: VERSION ==========

type VersionCmd struct{}

func (v *VersionCmd) Run(ctx *kong.Context) error {
	fmt.Println("termuxcam v1.0.0")
	fmt.Println("Go project to access frontal camera from termux")
	return nil
}

// ========== VARIÁVEIS GLOBAIS ==========

var (
	cli       CLI
	appConfig Config
	appState  AppState
	bot       *TelegramBot
	mu        sync.RWMutex
)

type Config struct {
	CaptureInterval time.Duration
	CameraMode      int // 0=back, 1=front, 2=both
	BurstCount      int
	KeepLogsDays    int
	EncryptionKey   string
}

type AppState struct {
	LastCapture   time.Time
	ErrorCount    int
	TotalPhotos   int
	FailedUploads int
	StartTime     time.Time
}

type TelegramBot struct {
	Token  string
	ChatID string
}

// ========== CONFIGURAÇÃO ==========

func loadConfig(configFile string) error {
	mu.Lock()
	defer mu.Unlock()
	
	// Valores padrão
	appConfig = Config{
		CaptureInterval: 5 * time.Minute,
		CameraMode:      1,
		BurstCount:      1,
		KeepLogsDays:    3,
	}
	
	file, err := os.Open(configFile)
	if err != nil {
		log.Printf("⚠️ Arquivo de configuração não encontrado, usando padrões")
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
					appConfig.CaptureInterval = dur
				}
			}
		case "camera":
			var mode int
			if _, err := fmt.Sscanf(value, "%d", &mode); err == nil && mode >= 0 && mode <= 2 {
				appConfig.CameraMode = mode
			}
		case "burst":
			var count int
			if _, err := fmt.Sscanf(value, "%d", &count); err == nil && count >= 1 && count <= 5 {
				appConfig.BurstCount = count
			}
		case "logs_days":
			var days int
			if _, err := fmt.Sscanf(value, "%d", &days); err == nil && days >= 1 && days <= 30 {
				appConfig.KeepLogsDays = days
			}
		case "encryption_key":
			appConfig.EncryptionKey = value
		}
	}
	
	log.Printf("✅ Configuração carregada: intervalo=%s, câmera=%d, burst=%d, logs=%d dias",
		appConfig.CaptureInterval, appConfig.CameraMode, appConfig.BurstCount, appConfig.KeepLogsDays)
	
	return scanner.Err()
}

func getConfig() Config {
	mu.RLock()
	defer mu.RUnlock()
	return appConfig
}

func setConfigValue(setting string) error {
	parts := strings.SplitN(setting, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("formato inválido, use: chave=valor")
	}
	
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	
	mu.Lock()
	defer mu.Unlock()
	
	switch key {
	case "camera":
		var mode int
		if _, err := fmt.Sscanf(value, "%d", &mode); err != nil || mode < 0 || mode > 2 {
			return fmt.Errorf("camera deve ser 0, 1 ou 2")
		}
		appConfig.CameraMode = mode
	case "interval":
		dur, err := time.ParseDuration(value)
		if err != nil || dur < time.Minute || dur > 24*time.Hour {
			return fmt.Errorf("intervalo inválido (1min-24h)")
		}
		appConfig.CaptureInterval = dur
	case "burst":
		var count int
		if _, err := fmt.Sscanf(value, "%d", &count); err != nil || count < 1 || count > 5 {
			return fmt.Errorf("burst deve ser entre 1 e 5")
		}
		appConfig.BurstCount = count
	case "logs_days":
		var days int
		if _, err := fmt.Sscanf(value, "%d", &days); err != nil || days < 1 || days > 30 {
			return fmt.Errorf("logs_days deve ser entre 1 e 30")
		}
		appConfig.KeepLogsDays = days
	default:
		return fmt.Errorf("chave desconhecida: %s", key)
	}
	
	// Salvar no arquivo
	return saveConfig()
}

func saveConfig() error {
	content := fmt.Sprintf(`# Configuração termuxcam
capture=%s
camera=%d
burst=%d
logs_days=%d
`, appConfig.CaptureInterval, appConfig.CameraMode, appConfig.BurstCount, appConfig.KeepLogsDays)
	
	if appConfig.EncryptionKey != "" {
		content += fmt.Sprintf("encryption_key=%s\n", appConfig.EncryptionKey)
	}
	
	return os.WriteFile(cli.ConfigFile, []byte(content), 0644)
}

// ========== .ENV (Melhoria 19) ==========

func loadEnvFile(envPath string) error {
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

// ========== TELEGRAM BOT ==========

func initBot() {
	token := os.Getenv("TG_BOT_TOKEN")
	chatID := os.Getenv("TG_CHAT_ID")
	
	if token != "" && chatID != "" {
		bot = &TelegramBot{Token: token, ChatID: chatID}
		log.Println("✅ Bot do Telegram inicializado")
	} else {
		log.Println("⚠️ Variáveis do Telegram não configuradas")
	}
}

func (b *TelegramBot) SendPhoto(filePath, caption string) error {
	// Usando stdlib para multipart form
	var buffer bytes.Buffer
	writer := io.MultiWriter(&buffer)
	
	boundary := fmt.Sprintf("----WebKitFormBoundary%d", time.Now().UnixNano())
	
	// Escrever campos
	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"chat_id\"\r\n\r\n%s\r\n", b.ChatID)
	
	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"caption\"\r\n\r\n%s\r\n", caption)
	
	// Ler arquivo
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
	
	// Enviar requisição
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

// ========== CAPTURA ==========

func captureBurst(cameraID string, count int) ([]string, error) {
	var files []string
	tempDir := filepath.Join(getBinaryDir(), "camera_captures", "temp")
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
		
		// Verificar se o arquivo não está vazio
		if info, err := os.Stat(filePath); err == nil && info.Size() > 0 {
			files = append(files, filePath)
		}
		
		time.Sleep(500 * time.Millisecond)
	}
	
	return files, nil
}

func doCapture() {
	log.Println("📸 Iniciando ciclo de captura...")
	
	cameras := []string{}
	config := getConfig()
	
	switch config.CameraMode {
	case 0:
		cameras = []string{backCameraHWID}
	case 1:
		cameras = []string{frontCameraHWID}
	case 2:
		cameras = []string{frontCameraHWID, backCameraHWID}
	}
	
	for _, cameraID := range cameras {
		cameraLabel := map[string]string{frontCameraHWID: "front", backCameraHWID: "back"}[cameraID]
		
		files, err := captureBurst(cameraID, config.BurstCount)
		if err != nil {
			log.Printf("❌ Erro na captura: %v", err)
			mu.Lock()
			appState.ErrorCount++
			mu.Unlock()
			continue
		}
		
		for _, filePath := range files {
			// Adicionar metadados EXIF (Melhoria 15)
			if err := addExifMetadata(filePath); err != nil {
				log.Printf("⚠️ Erro ao adicionar metadados: %v", err)
			}
			
			// Criptografar (Melhoria 1)
			if config.EncryptionKey != "" {
				encrypted, err := encryptFile(filePath, config.EncryptionKey)
				if err != nil {
					log.Printf("❌ Erro na criptografia: %v", err)
					continue
				}
				
				encPath := filepath.Join(getBinaryDir(), "camera_captures", "encrypted",
					filepath.Base(filePath)+".enc")
				if err := os.WriteFile(encPath, []byte(encrypted), 0644); err != nil {
					log.Printf("❌ Erro ao salvar arquivo criptografado: %v", err)
				}
			}
			
			// Enviar para Telegram
			if bot != nil {
				caption := fmt.Sprintf("📸 %s Camera\n🕐 %s",
					map[string]string{frontCameraHWID: "Frontal", backCameraHWID: "Traseira"}[cameraID],
					time.Now().Format("02/01/2006 15:04:05"))
				
				if err := bot.SendPhoto(filePath, caption); err != nil {
					log.Printf("❌ Erro no upload: %v", err)
					mu.Lock()
					appState.FailedUploads++
					mu.Unlock()
				} else {
					mu.Lock()
					appState.TotalPhotos++
					appState.LastCapture = time.Now()
					mu.Unlock()
					log.Printf("✅ Foto enviada: %s", filepath.Base(filePath))
				}
			}
			
			os.Remove(filePath)
		}
	}
	
	// Limpar temp
	tempDir := filepath.Join(getBinaryDir(), "camera_captures", "temp")
	os.RemoveAll(tempDir)
	os.MkdirAll(tempDir, 0755)
	
	// Rotacionar logs (Melhoria 4)
	rotateLogs()
}

// ========== CRIPTOGRAFIA (Melhoria 1) ==========

func encryptFile(filePath, key string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// ========== ROTAÇÃO DE LOGS (Melhoria 4) ==========

func rotateLogs() {
	config := getConfig()
	if config.KeepLogsDays <= 0 {
		config.KeepLogsDays = 3
	}
	
	cutoff := time.Now().AddDate(0, 0, -config.KeepLogsDays)
	logDir := filepath.Join(getBinaryDir(), "camera_captures")
	
	filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".log") && info.ModTime().Before(cutoff) {
			os.Remove(path)
			log.Printf("🗑️ Log antigo removido: %s", path)
		}
		return nil
	})
}

// ========== METADADOS EXIF (Melhoria 15) ==========

func addExifMetadata(filePath string) error {
	// Usar exiftool se disponível
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
	
	// Fallback: jhead
	if _, err := exec.LookPath("jhead"); err == nil {
		cmd := exec.Command("jhead",
			fmt.Sprintf("-ts%s", time.Now().Format("2006:01:02_15:04:05")),
			filePath,
		)
		return cmd.Run()
	}
	
	return nil // Sem ferramentas, ignorar
}

// ========== STATUS (Melhoria 9) ==========

func getStatus() map[string]interface{} {
	mu.RLock()
	defer mu.RUnlock()
	
	return map[string]interface{}{
		"status":         "running",
		"last_capture":   appState.LastCapture.Format(time.RFC3339),
		"total_photos":   appState.TotalPhotos,
		"error_count":    appState.ErrorCount,
		"failed_uploads": appState.FailedUploads,
		"uptime":         time.Since(appState.StartTime).Round(time.Second).String(),
		"config":         appConfig,
	}
}

func getBinaryDir() string {
	execPath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(execPath)
}

// ========== API SERVER (Usando stdlib, sem Gorilla) ==========

func startAPIServer(port int) {
	http.HandleFunc("/status", handleStatusAPI)
	http.HandleFunc("/health", handleHealthAPI)
	http.HandleFunc("/config", handleConfigAPI)
	http.HandleFunc("/capture", handleCaptureAPI)
	
	log.Printf("🌐 API Server iniciado na porta %d", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func handleStatusAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getStatus())
}

func handleHealthAPI(w http.ResponseWriter, r *http.Request) {
	config := getConfig()
	health := map[string]interface{}{
		"status":    "healthy",
		"camera":    "OK",
		"interval":  config.CaptureInterval.String(),
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
			setConfigValue(k + "=" + v)
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getConfig())
}

func handleCaptureAPI(w http.ResponseWriter, r *http.Request) {
	go doCapture()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "capture triggered"})
}

// ========== DAEMON / LOOP PRINCIPAL ==========

func runDaemon() {
	log.Printf("🔄 Iniciando loop de captura (intervalo: %v)", appConfig.CaptureInterval)
	
	ticker := time.NewTicker(appConfig.CaptureInterval)
	defer ticker.Stop()
	
	// Executar captura imediatamente
	doCapture()
	
	for range ticker.C {
		doCapture()
	}
}

func runOnce() {
	log.Println("📸 Executando captura única...")
	doCapture()
	log.Println("✅ Captura concluída")
}

// ========== MAIN ==========

func main() {
	// Inicializar estado
	appState.StartTime = time.Now()
	
	// Configurar logging
	logFile := filepath.Join(getBinaryDir(), "camera_captures", "capture.log")
	os.MkdirAll(filepath.Dir(logFile), 0755)
	
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		defer f.Close()
		log.SetOutput(f)
	}
	
	// Parse com Kong
	ctx := kong.Parse(&cli,
		kong.Name("termuxcam"),
		kong.Description("GO project to access frontal camera from termux"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)
	
	// Executar comando
	err = ctx.Run()
	if err != nil {
		log.Fatalf("❌ Erro: %v", err)
	}
}
