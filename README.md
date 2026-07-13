# termuxcam
GO project to access frontal camera from termux


## 1. Install

```sh
pkg update

pkg install golang
```

## 2. Verify

```sh
go version
```

## 3. Set up your workspace (Go modules, no GOPATH juggling needed for modern Go)
### Termux sets GOPATH to ~/go by default. Confirm with:

```sh
go env GOPATH
```

### Add Go's bin directory to your PATH so installed binaries (go install ...) are runnable directly:

```sh
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc

source ~/.bashrc
```
### (swap .bashrc for .zshrc if you're on zsh in Termux)

---

### 1. Instalar termux-services:

```
pkg install termux-services
```

### 2. Criar o diretório de serviço:

```
mkdir -p ~/.termux/service/capture_loop/log
```

### 3. Script run (o que o runit vai executar):

```
cat <<'EOF' > ~/.termux/service/capture_loop/run
#!/data/data/com.termux/files/usr/bin/sh
export TG_BOT_TOKEN="123456:ABC-your-token"
export TG_CHAT_ID="your_chat_id"
exec /data/data/com.termux/files/home/capture_loop
EOF

chmod +x ~/.termux/service/capture_loop/run
```

### 4. Habilitar e iniciar:

```sh
sv-enable capture_loop

sv up capture_loop
```

### 5. Verificar status / logs:

```sh
sv status capture_loop

tail -f ~/.termux/var/service/capture_loop/log/main/current
```

# How to build

```
go build -o termuxcapture main.go context.go
```
