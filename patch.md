cd ~/termuxcam
git checkout -b feature/mqtt
git apply --check mqtt.patch   # verifica se aplica limpo
git apply mqtt.patch
go get github.com/eclipse/paho.mqtt.golang
go mod tidy
go build -o build/termuxcam ./cmd/termuxcam