# Builder Stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy modules manifests
COPY go.mod go.sum* ./
# Download modules (if go.sum exists, otherwise it will update on build)
# Ensuring we have the lib
RUN go get github.com/go-telegram-bot-api/telegram-bot-api/v5

COPY . .

# Build the binary
# CGO_ENABLED=0 creates a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o bot main.go

# Final Stage
FROM alpine:latest

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/bot .

# Install certificates for HTTPS (Telegram API requires SSL)
RUN apk --no-cache add ca-certificates

# Create directory for persistent data
RUN mkdir -p /data

# Expose volume
VOLUME ["/data"]

# Command to run
CMD ["./bot"]