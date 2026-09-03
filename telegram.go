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
