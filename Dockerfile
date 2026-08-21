FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

# Copiar apenas go.mod e go.sum primeiro (para cache)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Compilar
RUN go build -o termuxcam main.go

# Imagem final
FROM alpine:latest

RUN apk add --no-cache bash curl exiftool

COPY --from=builder /app/termuxcam /usr/local/bin/
COPY termuxcam.conf /etc/termuxcam.conf
COPY .env.example /etc/.env

WORKDIR /data

ENTRYPOINT ["termuxcam"]
CMD ["start", "--daemon"]
