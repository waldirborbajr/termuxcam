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
