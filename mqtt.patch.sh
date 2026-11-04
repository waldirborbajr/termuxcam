#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
git checkout -b feature/mqtt 2>/dev/null || true

# ---------- 1. mqtt.go novo ----------
cat > internal/termuxcam/mqtt.go <<'GOEOF'
package termuxcam

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const mqttPrefix = "casa"

var (
	mqttMu    sync.RWMutex
	mqttTemp  = map[string]float64{}
	mqttHum   = map[string]float64{}
	mqttWhen  = map[string]time.Time{}
	mqttAlive = map[string]bool{}
)

// StartMQTT connects to the broker and caches retained sensor readings.
// No-op if mqtt_broker is empty. Call it in a goroutine.
func StartMQTT() {
	broker := GetConfig().MQTTBroker
	if broker == "" {
		log.Println("MQTT: no broker configured, sensor readings disabled")
		return
	}

	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID("termuxcam").
		SetAutoReconnect(true).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Printf("MQTT: connected to %s", broker)
			topics := map[string]byte{
				mqttPrefix + "/+/temperatura": 1,
				mqttPrefix + "/+/umidade":     1,
				mqttPrefix + "/+/status":      1,
			}
			c.SubscribeMultiple(topics, onMQTT)
		})

	if u := GetConfig().MQTTUser; u != "" {
		opts.SetUsername(u)
	}
	if p := GetConfig().MQTTPass; p != "" {
		opts.SetPassword(p)
	}

	if t := mqtt.NewClient(opts).Connect(); t.Wait() && t.Error() != nil {
		log.Printf("MQTT: connect failed: %v", t.Error())
	}
}

func onMQTT(_ mqtt.Client, m mqtt.Message) {
	parts := strings.Split(m.Topic(), "/")
	if len(parts) != 3 {
		return
	}
	room, field := parts[1], parts[2]
	payload := strings.TrimSpace(string(m.Payload()))
	var v float64
	fmt.Sscanf(payload, "%f", &v)

	mqttMu.Lock()
	defer mqttMu.Unlock()
	switch field {
	case "temperatura":
		mqttTemp[room] = v
		mqttWhen[room] = time.Now()
	case "umidade":
		mqttHum[room] = v
		mqttWhen[room] = time.Now()
	case "status":
		mqttAlive[room] = payload == "online"
	}
}
GOEOF

# ---------- 2. config.go ----------
perl -0pi -e 's/(\tDebug\s+bool\s+`toml:"debug"`\n)/$1\tMQTTBroker       string        `toml:"mqtt_broker"`\n\tMQTTUser         string        `toml:"mqtt_user"`\n\tMQTTPass         string        `toml:"mqtt_pass" json:"-"`\n/' internal/termuxcam/config.go

perl -0pi -e 's/(\t\tDebug\s+bool\s+`toml:"debug"`\n)/$1\t\tMQTTBroker       string `toml:"mqtt_broker"`\n\t\tMQTTUser         string `toml:"mqtt_user"`\n\t\tMQTTPass         string `toml:"mqtt_pass"`\n/' internal/termuxcam/config.go

perl -0pi -e 's/(\tAppConfig\.Debug = parsed\.Debug\n)/$1\tAppConfig.MQTTBroker = parsed.MQTTBroker\n\tAppConfig.MQTTUser = parsed.MQTTUser\n\tAppConfig.MQTTPass = parsed.MQTTPass\n/' internal/termuxcam/config.go

# ---------- 3. cli.go ----------
perl -0pi -e 's/(\tStartTelegramPoller\(\)\n)(\tif r\.Port > 0 \{)/$1\tgo StartMQTT()\n$2/' internal/termuxcam/cli.go

# ---------- 4. daemon.go ----------
perl -0pi -e 's/(\tInitBot\(\)\n\tStartTelegramPoller\(\)\n)(\n\tif port > 0 \{)/$1\tgo StartMQTT()\n$2/' internal/termuxcam/daemon.go

# ---------- 5. telegram_poll.go ----------
# 5a: helpText
perl -0pi -e 's{(\/rasplab - Raspberry Pi information\n)}{$1/temp [room] - sensor readings (all rooms or one)\n}' internal/termuxcam/telegram_poll.go

# 5b: case no switch
perl -0pi -e 's{(\tcase "rasplab":\n\t\treturn rasplabCommand\(\)\n)}{$1\tcase "temp":\n\t\treturn tempCommand(args)\n}' internal/termuxcam/telegram_poll.go

# 5c: função tempCommand antes de formatRasplabMemory
cat > /tmp/tempcmd.txt <<'GOEOF'
func tempCommand(args []string) string {
	mqttMu.RLock()
	defer mqttMu.RUnlock()

	if len(mqttTemp) == 0 && len(mqttHum) == 0 {
		return "No sensor readings yet. Check that the ESP32s are publishing and MQTT is connected."
	}

	rooms := []string{}
	if len(args) > 0 {
		rooms = []string{strings.ToLower(args[0])}
	} else {
		seen := map[string]bool{}
		for r := range mqttTemp {
			seen[r] = true
		}
		for r := range mqttHum {
			seen[r] = true
		}
		for r := range seen {
			rooms = append(rooms, r)
		}
	}

	var sb strings.Builder
	sb.WriteString("🌡 <b>Sensor readings</b>\n" + rule + "\n")
	for _, room := range rooms {
		badge := "🔴"
		if mqttAlive[room] {
			badge = "🟢"
		}
		when := ""
		if t, ok := mqttWhen[room]; ok {
			when = " · " + t.Format("15:04")
		}
		sb.WriteString(fmt.Sprintf("• <b>%s</b> %s <code>%.1f °C</code> / <code>%.0f %%</code>%s\n",
			html.EscapeString(room), badge, mqttTemp[room], mqttHum[room], when))
	}
	sb.WriteString("\n" + hintFooter)
	return sb.String()
}

GOEOF

# insere o conteúdo do arquivo temporário antes de "func formatRasplabMemory("
perl -0pi -e 'BEGIN { local $/; open my $fh, "<", "/tmp/tempcmd.txt" or die $!; $f = <$fh>; close $fh } s/(func formatRasplabMemory\()/$f$1/' internal/termuxcam/telegram_poll.go

# ---------- 6. gofmt + build + diff ----------
gofmt -w internal/termuxcam/mqtt.go internal/termuxcam/config.go internal/termuxcam/cli.go internal/termuxcam/daemon.go internal/termuxcam/telegram_poll.go
go build ./... 2>&1 || { echo "BUILD FALHOU — corrija antes de gerar o patch"; exit 1; }

git add -A
git diff --cached > mqtt.patch
echo
echo "Patch gerado: mqtt.patch"
echo "Para revisar:   git diff --cached"
echo "Para commitar:  git commit -m 'feat: MQTT sensor readings + /temp command'"
echo "Para reverter:  git reset --hard HEAD && git checkout -"