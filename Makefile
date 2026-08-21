.PHONY: build build-all clean install-termux test

BINARY_NAME=termuxcam
BUILD_DIR=./build

build:
	go build -o $(BINARY_NAME) .

build-all:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .
	GOOS=android GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-android-arm64 .
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .

clean:
	rm -f $(BINARY_NAME)
	rm -rf $(BUILD_DIR)

install-termux:
	pkg update && pkg upgrade -y
	pkg install golang git termux-api
	go build -o ~/bins/$(BINARY_NAME) .
	cp termuxcam.conf ~/bins/
	cp .env.example ~/bins/.env
	@echo "✅ Instalação concluída!"

test:
	go test -v ./...

help:
	@echo "Comandos:"
	@echo "  make build          - Compilar para arquitetura atual"
	@echo "  make build-all      - Compilar para múltiplas plataformas"
	@echo "  make install-termux - Instalar no Termux"
	@echo "  make clean          - Limpar arquivos compilados"
	@echo "  make test           - Executar testes"
