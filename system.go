package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

func getDiskUsage() string {
	out, _ := exec.Command("df", "-h", outputDir).Output()
	lines := strings.Split(string(out), "\n")
	if len(lines) > 1 {
		f := strings.Fields(lines[1])
		if len(f) >= 4 {
			return fmt.Sprintf("%s / %s", f[2], f[1])
		}
	}
	return "Unknown"
}

func getFolderUsage() string {
	out, _ := exec.Command("du", "-sh", outputDir).Output()
	parts := strings.Fields(string(out))
	if len(parts) > 0 {
		return parts[0]
	}
	return "Unknown"
}

func getMemoryUsage() string {
	out, _ := exec.Command("free", "-h").Output()
	lines := strings.Split(string(out), "\n")
	if len(lines) > 1 {
		f := strings.Fields(lines[1])
		if len(f) >= 3 {
			return fmt.Sprintf("%s / %s", f[2], f[1])
		}
	}
	return "Unknown"
}

func getCPUUsagePercent() string {
	out, _ := exec.Command("top", "-bn1").Output()
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Cpu(s)") {
			parts := strings.Split(line, ",")
			if len(parts) > 0 {
				idleStr := strings.TrimSpace(strings.Split(parts[len(parts)-1], "%")[0])
				if idle, err := strconv.ParseFloat(idleStr, 64); err == nil {
					return fmt.Sprintf("%.1f%%", 100-idle)
				}
			}
		}
	}
	return "N/A"
}

func getCPUTemperature() string {
	out, err := exec.Command("cat", "/sys/class/thermal/thermal_zone0/temp").Output()
	if err == nil {
		if temp, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			return fmt.Sprintf("%.1f°C", float64(temp)/1000)
		}
	}
	return "N/A"
}

func getLocalIP() string {
	out, _ := exec.Command("ip", "route", "get", "1").Output()
	parts := strings.Fields(string(out))
	for i, p := range parts {
		if p == "src" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "N/A"
}

func getPublicIP() string {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "N/A"
	}
	defer resp.Body.Close()
	ip, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(ip))
}

func getDeviceInfo() string {
	model, _ := exec.Command("getprop", "ro.product.model").Output()
	android, _ := exec.Command("getprop", "ro.build.version.release").Output()
	return fmt.Sprintf("%s (Android %s)", strings.TrimSpace(string(model)), strings.TrimSpace(string(android)))
}

func getWifiStatus() string {
	out, err := exec.Command("termux-wifi-connectioninfo").Output()
	if err != nil {
		return "N/A"
	}
	var wifi struct {
		SupplicantState string `json:"supplicantState"`
		Rssi            int    `json:"rssi"`
	}
	json.Unmarshal(out, &wifi)
	if wifi.SupplicantState == "COMPLETED" {
		return fmt.Sprintf("Connected (signal: %d dBm)", wifi.Rssi)
	}
	return "Disconnected"
}

func getBatteryInfo() string {
	out, err := exec.Command("termux-battery-status").Output()
	if err != nil {
		return "N/A"
	}
	var bat struct {
		Percentage  int     `json:"percentage"`
		Temperature float64 `json:"temperature"`
		Health      string  `json:"health"`
	}
	json.Unmarshal(out, &bat)
	return fmt.Sprintf("%d%% | %.1f°C | %s", bat.Percentage, bat.Temperature, bat.Health)
}
