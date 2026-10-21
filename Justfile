default:
    @just --list

build:
    go build -o termuxcam .

build-all:
    mkdir -p build
    GOOS=linux GOARCH=amd64 go build -o build/termuxcam-linux-amd64 .
    GOOS=linux GOARCH=arm64 go build -o build/termuxcam-linux-arm64 .
    GOOS=android GOARCH=arm64 go build -o build/termuxcam-android-arm64 .
    GOOS=windows GOARCH=amd64 go build -o build/termuxcam-windows-amd64.exe .

clean:
    rm -f termuxcam
    rm -rf build

install-termux:
    pkg update && pkg upgrade -y
    pkg install golang git termux-api termux-services
    mkdir -p "$HOME/bins"
    go build -o "$HOME/bins/termuxcam" .
    if [ ! -f "$HOME/bins/termuxcam.conf" ]; then cp termuxcam.conf "$HOME/bins/termuxcam.conf"; fi
    if [ ! -f "$HOME/bins/.env" ]; then cp .env.example "$HOME/bins/.env"; fi
    chmod 600 "$HOME/bins/.env"
    @echo "Installation complete. Review ~/bins/.env before starting the service."

test:
    go test -v ./...

start:
    ./termuxcam-ctl start

stop:
    ./termuxcam-ctl stop

restart:
    ./termuxcam-ctl restart

status:
    ./termuxcam-ctl status

logs:
    ./termuxcam-ctl logs

install-service:
    ./termuxcam-ctl install

uninstall-service:
    ./termuxcam-ctl uninstall
