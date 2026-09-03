#!/bin/bash
set -e
echo "🔨 Compilando termuxcam..."
go build -o ~/bins/termuxcam .
echo "♻️ Reiniciando serviço..."
sv restart termuxcam
echo "📋 Status do serviço:"
sv status termuxcam
echo "📄 Últimas linhas do log:"
tail -5 ~/camera_captures/capture.log
