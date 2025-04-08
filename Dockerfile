FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build-linux-amd64

FROM alpine:latest AS runner
WORKDIR /app
VOLUME [ "/app/config", "/app/cert" ]
ARG BUILD_APP
COPY --from=builder /app/build/linux/amd64/gen-cert .
COPY --from=builder /app/build/linux/amd64/${BUILD_APP} ./app
CMD ["./app", "--config", "./config/config.json"]

