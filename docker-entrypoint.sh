#!/bin/sh
# Script de entrada para o container Docker

set -e

# Configurar variáveis de ambiente
export TG_BOT_TOKEN=${TG_BOT_TOKEN:-""}
export TG_CHAT_ID=${TG_CHAT_ID:-""}

# Criar diretórios
mkdir -p /data/camera_captures

# Copiar configuração se não existir
if [ ! -f /config/termuxcam.conf ]; then
    cp /etc/termuxcam.conf /config/termuxcam.conf
fi

# Gerar chave de criptografia se não existir
if [ ! -f /config/encryption.key ]; then
    openssl rand -base64 32 > /config/encryption.key
    chmod 600 /config/encryption.key
fi

# Exportar chave de criptografia
export ENCRYPTION_KEY=$(cat /config/encryption.key)

# Executar o binário
exec /usr/local/bin/termuxcam-linux-amd64 "$@"
