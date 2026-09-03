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
