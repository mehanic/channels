FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o rtmp-server cmd/server/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates ffmpeg

WORKDIR /app

COPY --from=builder /app/rtmp-server .
COPY --from=builder /app/config.yaml .

RUN mkdir -p /app/hls

EXPOSE 1935 8080 9090

CMD ["./rtmp-server", "-config", "config.yaml"] 