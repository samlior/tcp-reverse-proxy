FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN make build PLATFORMS=${TARGETOS}/${TARGETARCH}

FROM alpine:latest AS runner
WORKDIR /app
VOLUME [ "/app/config", "/app/cert" ]
ARG BUILD_APP
ARG TARGETOS
ARG TARGETARCH
COPY --from=builder /app/build/${TARGETOS}/${TARGETARCH}/gen-cert .
COPY --from=builder /app/build/${TARGETOS}/${TARGETARCH}/${BUILD_APP} ./app
CMD ["./app", "--config", "./config/config.json"]

