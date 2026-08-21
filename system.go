package main

import (
	"bufio"
	"fmt"
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
