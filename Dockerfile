FROM golang:1.24.1 as builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN make build-linux-amd64

FROM alpine:latest as runner
RUN adduser -D appuser
USER appuser
WORKDIR /app

FROM runner as entry-point
COPY --from=builder /app/build/linux/amd64/entry-point .
ENTRYPOINT ["./entry-point", "--config", "config/config.json"]

FROM runner as relay-server
COPY --from=builder /app/build/linux/amd64/relay-server .
ENTRYPOINT ["./relay-server", "--config", "config/config.json"]

FROM runner as reverse-proxy
COPY --from=builder /app/build/linux/amd64/reverse-proxy .
ENTRYPOINT ["./reverse-proxy", "--config", "config/config.json"]

