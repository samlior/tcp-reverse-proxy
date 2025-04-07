package main

import (
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	entry_point "github.com/samlior/tcp-reverse-proxy/pkg/entry-point"
)

func main() {
	serverCert := flag.String("server-cert", "cert/server.crt", "server certificate path")
	authPrivateKey := flag.String("auth-private-key", "cert/auth", "auth private key path")
	serverAddress := flag.String("server-address", "localhost:4433", "server address")
	strRoutes := flag.String("routes", "", "route addresses, separated by commas")

	flag.Parse()

	serverCertBytes, err := os.ReadFile(*serverCert)
	if err != nil {
		log.Fatal("failed to read server certificate:", err)
	}
	authPrivateKeyBytes, err := os.ReadFile(*authPrivateKey)
	if err != nil {
		log.Fatal("failed to read auth private key:", err)
	}

	certPool := x509.NewCertPool()
	ok := certPool.AppendCertsFromPEM(serverCertBytes)
	if !ok {
		log.Fatal("failed to append the server certificate")
	}

	routes, err := entry_point.ParseRoutes(strings.Split(*strRoutes, ","))
	if err != nil {
		log.Fatal("failed to parse routes:", err)
	}

	entryPointServer := entry_point.NewEntryPointServer(*serverAddress, authPrivateKeyBytes, certPool, routes)

	go common.HandleSignal(entryPointServer)

	go entryPointServer.KeepDialing()

	for _, route := range routes {
		srcHost := route.SrcHost
		if srcHost == "*" {
			srcHost = "0.0.0.0"
		}

		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", srcHost, route.SrcPort))
		if err != nil {
			log.Fatal("failed to listen:", err)
		}

		log.Printf("listening on %s:%d...", srcHost, route.SrcPort)

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Println("failed to accept connection:", err)
				continue
			}

			go entryPointServer.HandleConnection(conn)
		}
	}
}
