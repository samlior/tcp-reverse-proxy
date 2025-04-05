package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"

	relay_server "github.com/samlior/tcp-reverse-proxy/pkg/relay-server"
)

func main() {
	serverCert := flag.String("server-cert", "cert/server.crt", "server certificate path")
	serverKey := flag.String("server-key", "cert/server.key", "server key path")
	authPublicKey := flag.String("auth-public-key", "cert/auth.pub", "auth public key path")
	host := flag.String("host", "0.0.0.0", "host")
	port := flag.Int("port", 4433, "port")

	flag.Parse()

	serverCertBytes, err := os.ReadFile(*serverCert)
	if err != nil {
		log.Fatal("failed to read server certificate:", err)
	}
	serverKeyBytes, err := os.ReadFile(*serverKey)
	if err != nil {
		log.Fatal("failed to read server key:", err)
	}
	authPublicKeyBytes, err := os.ReadFile(*authPublicKey)
	if err != nil {
		log.Fatal("failed to read auth public key:", err)
	}

	cert, err := tls.X509KeyPair(serverCertBytes, serverKeyBytes)
	if err != nil {
		log.Fatal("failed to create x509 key pair:", err)
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf("%s:%d", *host, *port), &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		log.Fatal("failed to listen:", err)
	}

	defer listener.Close()

	log.Printf("listening on %s:%d...", *host, *port)

	relayServer := relay_server.NewRelayServer(authPublicKeyBytes)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("failed to accept connection:", err)
			continue
		}

		go relayServer.HandleConnection(conn)
	}
}
