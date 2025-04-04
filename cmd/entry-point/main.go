package main

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"flag"
	"log"
	"os"
	"time"
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
	ok := certPool.AppendCertsFromPEM(serverCertBytes)
	if !ok {
		log.Fatal("failed to append the server certificate")
	}

	conn, err := tls.Dial("tcp", *serverAddress, &tls.Config{
		RootCAs: certPool,
	})
	if err != nil {
		log.Fatal("failed to connect to relay server:", err)
	}

	defer conn.Close()

	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		log.Fatal("failed to read from relay server:", err)
	}

	signature := ed25519.Sign(authPrivateKeyBytes, buffer[:n])

	write := func(data []byte) (int, error) {
		prefix := make([]byte, 2)
		binary.BigEndian.PutUint16(prefix, uint16(len(data)))
		return conn.Write(append(prefix, data...))
	}

	n, err = write(signature)
	if err != nil {
		log.Fatal("failed to write to relay server:", err)
	}

	log.Println("wrote size:", n)

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		<-tick.C
		write([]byte("hello"))
	}
}
