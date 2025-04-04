package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
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
		log.Fatalf("failed to create x509 key pair:", err)
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf("%s:%d", *host, *port), &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	defer listener.Close()

	log.Printf("listening on %s:%d...", *host, *port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("failed to accept connection:", err)
			continue
		}
		go handleConnection(authPublicKeyBytes, conn)
	}
}

type Message struct {
	Flag uint8
	Data []byte
}

func parseMessage(bytes []byte) (*Message, error) {
	if len(bytes) < 2 {
		return nil, fmt.Errorf("invalid message")
	}

	return &Message{Flag: bytes[0], Data: bytes[1:]}, nil
}

func handleConnection(authPublicKeyBytes []byte, conn net.Conn) {
	defer conn.Close()

	log.Println("client connected:", conn.RemoteAddr())

	randomBytes := make([]byte, 32)

	_, err := rand.Read(randomBytes)
	if err != nil {
		log.Println("failed to generate random bytes:", err)
		return
	}

	// send challenge
	conn.Write(randomBytes)

	ch := make(chan []byte)

	go func() {
		defer close(ch)

		buffer := make([]byte, 1024)
		cursor := 0
		length := 0
		left := 0
		pending := []byte{}
		var err error

		read := func(n int) []byte {
			slice := buffer[cursor : cursor+n]
			cursor += n
			length -= n
			return slice
		}

		for {
			length, err = conn.Read(buffer)
			if err == io.EOF {
				// closed
				return
			}
			if err != nil {
				log.Println("error reading from client:", err)
				return
			}

			for length > 0 {
				if left == 0 {
					if length < 2 {
						log.Println("client sent invalid message")
						return
					}

					left = int(binary.BigEndian.Uint16(read(2)))
				}

				var readLength int
				if left > length {
					readLength = length
				} else {
					readLength = left
				}

				pending = append(pending, read(readLength)...)

				left -= readLength

				if left == 0 {
					ch <- pending

					pending = []byte{}
				}
			}

			cursor = 0
			length = 0
		}
	}()

	// wait for challenge answer
	select {
	case <-time.After(time.Second):
		log.Println("client challenge timed out")
		return
	case answer := <-ch:
		// verify challenge answer
		if !ed25519.Verify(authPublicKeyBytes, randomBytes, answer) {
			log.Println("client challenge verification failed")
			return
		}
	}

	for bytes := range ch {
		// TODO
		log.Println("received message:", string(bytes))
	}
}
