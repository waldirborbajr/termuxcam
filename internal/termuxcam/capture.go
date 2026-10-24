package termuxcam

import (
	"context"
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
	CaptureTimeout  = 45 * time.Second
)

func CaptureBurst(cameraID string, count int) ([]string, error) {
	if count < 1 || count > 5 {
		return nil, fmt.Errorf("burst count must be between 1 and 5")
	}

	var files []string
	tempDir := filepath.Join(GetBinaryDir(), "camera_captures", "temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("create capture directory: %w", err)
	}

	for i := 0; i < count; i++ {
		filename := fmt.Sprintf("%s_%d_%d.jpg",
			time.Now().Format("20060102_150405"),
			i+1,
			time.Now().UnixNano())
		filePath := filepath.Join(tempDir, filename)

		ctx, cancel := context.WithTimeout(context.Background(), CaptureTimeout)
		cmd := exec.CommandContext(ctx, "termux-camera-photo", "-c", cameraID, filePath)
		err := cmd.Run()
		cancel()
		if err != nil {
			if removeErr := os.Remove(filePath); removeErr != nil && !os.IsNotExist(removeErr) {
				log.Printf("Failed to remove partial capture: %v", removeErr)
			}
			return files, fmt.Errorf("capture %d failed: %w", i+1, err)
		}

		info, err := os.Stat(filePath)
		if err != nil || info.Size() == 0 {
			if removeErr := os.Remove(filePath); removeErr != nil && !os.IsNotExist(removeErr) {
				log.Printf("Failed to remove invalid capture: %v", removeErr)
			}
			if err != nil {
				return files, fmt.Errorf("inspect capture %d: %w", i+1, err)
			}
			return files, fmt.Errorf("capture %d produced an empty file", i+1)
		}
		files = append(files, filePath)

		time.Sleep(500 * time.Millisecond)
	}

	return files, nil
}

func DoCapture() {
	captureMu.Lock()
	defer captureMu.Unlock()
	log.Println("Starting capture cycle...")

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

	preserveTempFiles := false
	for _, cameraID := range cameras {
		files, err := CaptureBurst(cameraID, config.BurstCount)
		if err != nil {
			log.Printf("Capture failed: %v", err)
			IncrementErrorCount()
			continue
		}

		for _, filePath := range files {
			if err := AddExifMetadata(filePath); err != nil {
				log.Printf("Failed to add metadata: %v", err)
			}

			if config.EncryptionKey != "" {
				encrypted, err := EncryptFile(filePath, config.EncryptionKey)
				if err != nil {
					log.Printf("Encryption failed: %v", err)
					IncrementErrorCount()
					if preserveErr := preserveFailedPhoto(filePath); preserveErr != nil {
						log.Printf("Failed to preserve photo after encryption failure: %v", preserveErr)
						preserveTempFiles = true
					}
					continue
				}

				encPath := filepath.Join(GetBinaryDir(), "camera_captures", "encrypted",
					filepath.Base(filePath)+".enc")
				if err := os.WriteFile(encPath, []byte(encrypted), 0644); err != nil {
					log.Printf("Failed to save encrypted file: %v", err)
					IncrementErrorCount()
					if preserveErr := preserveFailedPhoto(filePath); preserveErr != nil {
						log.Printf("Failed to preserve photo after encryption write failure: %v", preserveErr)
						preserveTempFiles = true
					}
					continue
				}
			}

			if Bot != nil {
				caption := fmt.Sprintf("📸 %s camera\n🕐 %s",
					map[string]string{FrontCameraHWID: "Front", BackCameraHWID: "Back"}[cameraID],
					time.Now().Format("02/01/2006 15:04:05"))

				if err := Bot.SendPhoto(filePath, caption); err != nil {
					log.Printf("Photo upload failed: %v", err)
					IncrementFailedUploads()
					if err := preserveFailedPhoto(filePath); err != nil {
						log.Printf("Failed to preserve photo after upload failure: %v", err)
						preserveTempFiles = true
					}
				} else {
					IncrementTotalPhotos()
					SetLastCapture(time.Now())
					log.Printf("Photo sent: %s", filepath.Base(filePath))
				}
			} else {
				log.Printf("Telegram bot is not configured; preserving photo %s", filepath.Base(filePath))
				IncrementFailedUploads()
				if err := preserveFailedPhoto(filePath); err != nil {
					log.Printf("Failed to preserve photo without Telegram configuration: %v", err)
					preserveTempFiles = true
				}
			}

			if !preserveTempFiles {
				if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
					log.Printf("Failed to remove processed photo: %v", err)
				}
			}
		}
	}

	if !preserveTempFiles {
		tempDir := filepath.Join(GetBinaryDir(), "camera_captures", "temp")
		if err := os.RemoveAll(tempDir); err != nil {
			log.Printf("Failed to clear temporary captures: %v", err)
		} else if err := os.MkdirAll(tempDir, 0755); err != nil {
			log.Printf("Failed to recreate temporary capture directory: %v", err)
		}
	}

	RotateLogs()
}

func preserveFailedPhoto(filePath string) error {
	failedDir := filepath.Join(GetBinaryDir(), "camera_captures", "failed")
	if err := os.MkdirAll(failedDir, 0755); err != nil {
		return err
	}
	return os.Rename(filePath, filepath.Join(failedDir, filepath.Base(filePath)))
}

func RunDaemon() {
	config := GetConfig()
	log.Printf("Starting capture loop (interval: %v)", config.CaptureInterval)

	ticker := time.NewTicker(config.CaptureInterval)
	defer ticker.Stop()

	DoCapture()

	for range ticker.C {
		DoCapture()
	}
}

func RunOnce() {
	log.Println("Running one-shot capture...")
	DoCapture()
	log.Println("Capture complete")
}
