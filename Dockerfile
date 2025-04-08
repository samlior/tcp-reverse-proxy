FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build-linux-amd64

FROM alpine:latest AS runner
WORKDIR /app
ARG BUILD_APP
COPY --from=builder /app/build/linux/amd64/gen-cert .
COPY --from=builder /app/build/linux/amd64/${BUILD_APP} .
ENV BUILD_APP=${BUILD_APP}
VOLUME [ "/app/config", "/app/cert" ]
CMD ./$BUILD_APP --config config/config.json

