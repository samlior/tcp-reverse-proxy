.PHONY: build build-linux clean

all: build

build:
	go build -o bin/relay-server cmd/relay-server/main.go
	go build -o bin/reverse-proxy cmd/reverse-proxy/main.go
	go build -o bin/entry-point cmd/entry-point/main.go

build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/relay-server cmd/relay-server/main.go
	GOOS=linux GOARCH=amd64 go build -o bin/reverse-proxy cmd/reverse-proxy/main.go
	GOOS=linux GOARCH=amd64 go build -o bin/entry-point cmd/entry-point/main.go

clean:
	rm -rf bin

