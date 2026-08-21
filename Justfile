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
    pkg install golang git termux-api
    go build -o ~/bins/termuxcam .
    cp termuxcam.conf ~/bins/
    cp .env.example ~/bins/.env
    @echo "✅ Instalação concluída!"

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
