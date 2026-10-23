#!/bin/sh
# Script de entrada para o container Docker

set -e

# Criar diretórios
mkdir -p /data/camera_captures

# Executar o binário
exec /usr/local/bin/termuxcam "$@"
