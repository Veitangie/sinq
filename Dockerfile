# Stage 1: The Builder
FROM golang:1.26.4-alpine AS builder

# Install certificates for HTTPS requests and tzdata for time functions
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Pre-copy go.mod and go.sum to cache module downloads
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy the rest of the source code
COPY . .

# Build a statically linked binary (CGO_ENABLED=0) to ensure it runs anywhere
# -w -s flags strip debug info to minimize binary size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /go/bin/sinq ./cmd/sinq

# Stage 2: The Final Minimal Image
FROM alpine:latest

# Import the certificates from the builder stage
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /workspace

# Bring in the binary
COPY --from=builder /go/bin/sinq /usr/local/bin/sinq

# By default, act as the CLI tool
ENTRYPOINT ["sinq"]
