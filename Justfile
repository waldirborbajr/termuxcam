default:
    @just --list

build:
    mkdir -p build
    go build -o build/termuxcam ./cmd/termuxcam

build-all:
    mkdir -p build
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/termuxcam-linux-amd64 ./cmd/termuxcam
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/termuxcam-linux-arm64 ./cmd/termuxcam
    CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -o build/termuxcam-android-arm64 ./cmd/termuxcam
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o build/termuxcam-windows-amd64.exe ./cmd/termuxcam

clean:
    rm -rf build

install-termux:
    pkg update && pkg upgrade -y
    pkg install golang git termux-api termux-services
    mkdir -p build
    mkdir -p "$HOME/bins"
    go build -o build/termuxcam ./cmd/termuxcam
    cp build/termuxcam "$HOME/bins/termuxcam"
    if [ ! -f "$HOME/bins/termuxcam.toml" ]; then cp termuxcam.toml "$HOME/bins/termuxcam.toml"; fi
    chmod 600 "$HOME/bins/termuxcam.toml"
    @echo "Installation complete. Review ~/bins/termuxcam.toml before starting the service."

vet:
    go vet ./...

check: vet test

test:
    go test -v ./...

start:
    ./build/termuxcam start --daemon

stop:
    ./build/termuxcam stop

restart:
    ./build/termuxcam restart

status:
    ./build/termuxcam status

logs:
    tail -f camera_captures/capture.log
