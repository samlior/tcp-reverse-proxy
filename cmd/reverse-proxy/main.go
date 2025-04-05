package main

import (
	"crypto/x509"
	"flag"
	"log"
	"os"

	reverse_proxy "github.com/samlior/tcp-reverse-proxy/pkg/reverse-proxy"
)

func main() {
	serverCert := flag.String("server-cert", "cert/server.crt", "server certificate path")
	authPrivateKey := flag.String("auth-private-key", "cert/auth", "auth private key path")
	serverAddress := flag.String("server-address", "localhost:4433", "server address")

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
	if !certPool.AppendCertsFromPEM(serverCertBytes) {
		log.Fatal("failed to append server certificate to cert pool")
	}

	reverseProxyServer := reverse_proxy.NewReverseProxyServer(*serverAddress, authPrivateKeyBytes, certPool)

	go reverseProxyServer.KeepDialing()
}
