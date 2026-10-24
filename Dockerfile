FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN mkdir -p build && go build -o build/termuxcam ./cmd/termuxcam

FROM alpine:latest

RUN apk add --no-cache bash curl exiftool

COPY --from=builder /app/build/termuxcam /usr/local/bin/
COPY termuxcam.toml /usr/local/bin/termuxcam.toml

WORKDIR /data

ENTRYPOINT ["termuxcam"]
CMD ["start", "--daemon"]
