package main

import (
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	reverse_proxy "github.com/samlior/tcp-reverse-proxy/pkg/reverse-proxy"
)

var (
	BuildTime string
	GitCommit string
)

func main() {
	serverCert := flag.String("server-cert", "cert/server.crt", "server certificate path")
	authPrivateKey := flag.String("auth-private-key", "cert/auth", "auth private key path")
	serverAddress := flag.String("server-address", "localhost:4433", "server address")
	version := flag.Bool("version", false, "show version")

	flag.Parse()

	if *version {
		fmt.Printf("tcp-reverse-proxy/entry-point\n build time: %s +0\n git commit: %s\n", BuildTime, GitCommit)
		return
	}

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

	go common.HandleSignal(reverseProxyServer)

	go reverseProxyServer.KeepDialing()

	select {}
}
