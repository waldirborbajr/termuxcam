#!/bin/bash
# =============================================
# termuxcam - Build + Restart Script
# =============================================

set -e  # Para o script em caso de erro

echo "🚀 Iniciando build do termuxcam..."

# Diretórios
BIN_DIR="$HOME/bins"
SERVICE_DIR="$HOME/.termux/service/termuxcam"
BINARY="$BIN_DIR/termuxcam"

# Verifica se o diretório do binário existe
mkdir -p "$BIN_DIR"

# Build
echo "🔨 Compilando..."
go build -o "$BINARY" main.go

if [ $? -ne 0 ]; then
    echo "❌ Erro na compilação!"
    exit 1
fi

echo "✅ Build concluído com sucesso!"
chmod +x "$BINARY"

# Verifica se o serviço existe
if [ ! -d "$SERVICE_DIR" ]; then
    echo "⚠️  Serviço termuxcam não encontrado. Rode a instalação primeiro."
    exit 1
fi

# Reinicia o serviço
echo "🔄 Reiniciando serviço termuxcam..."

sv restart termuxcam

sleep 2

# Mostra status
echo "📊 Status do serviço:"
sv status termuxcam

echo ""
echo "📝 Últimas linhas do log:"
tail -n 15 "$HOME/camera_captures/capture.log" 2>/dev/null || echo "Log ainda não gerado."

echo ""
echo "✅ Pronto! O serviço foi recompilado e reiniciado."
