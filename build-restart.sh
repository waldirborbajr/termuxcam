#!/bin/bash
set -e
echo "🔨 Compilando termuxcam..."
mkdir -p "$HOME/bins"
go build -o ~/bins/termuxcam .
echo "♻️ Reiniciando serviço..."
sv restart termuxcam
echo "📋 Status do serviço:"
sv status termuxcam
echo "📄 Últimas linhas do log:"
tail -5 "$HOME/bins/camera_captures/capture.log"
