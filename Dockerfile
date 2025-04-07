FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build-linux-amd64

FROM alpine:latest AS runner
RUN adduser -D appuser
USER appuser
WORKDIR /app

# Apps
FROM runner AS entry-point
COPY --from=builder /app/build/linux/amd64/entry-point .
ENTRYPOINT ["./entry-point", "--config", "config/config.json"]

FROM runner AS relay-server
COPY --from=builder /app/build/linux/amd64/relay-server .
ENTRYPOINT ["./relay-server", "--config", "config/config.json"]

FROM runner AS reverse-proxy
COPY --from=builder /app/build/linux/amd64/reverse-proxy .
ENTRYPOINT ["./reverse-proxy", "--config", "config/config.json"]

