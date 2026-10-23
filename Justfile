default:
    @just --list

build:
    go build -o termuxcam ./cmd/termuxcam

build-all:
    mkdir -p build
    GOOS=linux GOARCH=amd64 go build -o build/termuxcam-linux-amd64 ./cmd/termuxcam
    GOOS=linux GOARCH=arm64 go build -o build/termuxcam-linux-arm64 ./cmd/termuxcam
    GOOS=android GOARCH=arm64 go build -o build/termuxcam-android-arm64 ./cmd/termuxcam
    GOOS=windows GOARCH=amd64 go build -o build/termuxcam-windows-amd64.exe ./cmd/termuxcam

clean:
    rm -f termuxcam
    rm -rf build

install-termux:
    pkg update && pkg upgrade -y
    pkg install golang git termux-api termux-services
    mkdir -p "$HOME/bins"
    go build -o "$HOME/bins/termuxcam" ./cmd/termuxcam
    if [ ! -f "$HOME/bins/termuxcam.toml" ]; then cp termuxcam.toml "$HOME/bins/termuxcam.toml"; fi
    chmod 600 "$HOME/bins/termuxcam.toml"
    @echo "Installation complete. Review ~/bins/termuxcam.toml before starting the service."

vet:
    go vet ./...

check: vet test

test:
    go test -v ./...

start:
    ./termuxcam start --daemon

stop:
    ./termuxcam stop

restart:
    ./termuxcam restart

status:
    ./termuxcam status

logs:
    tail -f camera_captures/capture.log
