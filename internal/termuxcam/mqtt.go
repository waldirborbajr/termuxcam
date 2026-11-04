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
