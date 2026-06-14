FROM golang:1.26.4-alpine AS builder

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /go/bin/sinq ./cmd/sinq

FROM alpine:latest

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /workspace

COPY --from=builder /go/bin/sinq /usr/local/bin/sinq

ENTRYPOINT ["sinq"]
