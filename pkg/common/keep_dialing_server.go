package common

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"math/rand"
	"time"

	constant "github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type KeepDialingServer struct {
	*CommonServer

	OnDial func(conn *Conn) error

	isUpstream bool

	semaphore           chan struct{}
	certPool            *x509.CertPool
	serverAddress       string
	authPrivateKeyBytes []byte
}

func NewKeepDialingServer(
	isUpstream bool,
	serverAddress string,
	authPrivateKeyBytes []byte,
	certPool *x509.CertPool) *KeepDialingServer {
	s := &KeepDialingServer{
		isUpstream:          isUpstream,
		semaphore:           make(chan struct{}, constant.Concurrency),
		serverAddress:       serverAddress,
		authPrivateKeyBytes: authPrivateKeyBytes,
		certPool:            certPool,
		CommonServer:        NewCommonServer(),
	}

	var keepDialingConnType string
	if isUpstream {
		keepDialingConnType = constant.ConnTypeUp
	} else {
		keepDialingConnType = constant.ConnTypeDown
	}

	s.OnConnClosed = func(conn *Conn) {
		// whenever a connection drops,
		// immediately establish a new one to maintain
		// a consistent number of pending connections
		if conn.Type == keepDialingConnType && conn.Status == constant.ConnStatusPending {
			go s.releaseSemaphore(1)
		}
	}

	s.OnConnected = func(conn *Conn, anotherConn *Conn) {
		// whenever two connection connect to each other,
		// immediately establish a new one to maintain
		// a consistent number of pending connections
		go s.releaseSemaphore(1)
	}

	return s
}

func (s *KeepDialingServer) releaseSemaphore(multiple int) {
	time.Sleep(time.Millisecond * time.Duration((rand.Intn(50)+50)*multiple))
	<-s.semaphore
}

func (s *KeepDialingServer) onDial(conn *Conn) error {
	if s.OnDial != nil {
		return s.OnDial(conn)
	}
	return nil
}

func (s *KeepDialingServer) dial() {
	conn, err := tls.Dial("tcp", s.serverAddress, &tls.Config{
		RootCAs: s.certPool,
	})
	if err != nil {
		log.Println("failed to dial to relay server:", err)
		go s.releaseSemaphore(100)
		return
	}

	var keepDialingConnType string
	if s.isUpstream {
		keepDialingConnType = constant.ConnTypeUp
	} else {
		keepDialingConnType = constant.ConnTypeDown
	}

	s.CommonServer.HandleConnection(conn, keepDialingConnType, func(conn *Conn) error {
		challenge := <-conn.Ch
		if len(challenge) != 32 {
			return errors.New("invalid challenge")
		}

		signature := ed25519.Sign(s.authPrivateKeyBytes, challenge)

		var flag byte
		if s.isUpstream {
			flag = 0x01
		} else {
			flag = 0x02
		}

		// this is the entry point
		// inform the relay server our type
		_, err = conn.Conn.Write(append([]byte{flag}, signature...))
		if err != nil {
			return err
		}

		// invoke the callback
		err = s.onDial(conn)
		if err != nil {
			return err
		}

		return nil
	})
}

func (s *KeepDialingServer) KeepDialing() {
	for {
		select {
		case <-s.Closed:
			return
		case s.semaphore <- struct{}{}:
			go s.dial()
		}
	}
}
