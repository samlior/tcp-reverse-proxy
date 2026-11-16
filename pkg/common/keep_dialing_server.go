package common

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"math/rand"
	"net"
	"time"

	constant "github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type KeepDialingServer struct {
	*CommonServer

	OnDial func(conn *Conn) error

	groupId    uint8
	isUpstream bool

	semaphore           chan struct{}
	certPool            *x509.CertPool
	serverAddress       string
	authPrivateKeyBytes []byte
}

func NewKeepDialingServer(
	groupId uint8,
	isUpstream bool,
	serverAddress string,
	authPrivateKeyBytes []byte,
	certPool *x509.CertPool) *KeepDialingServer {
	s := &KeepDialingServer{
		groupId:             groupId,
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
	timer := time.NewTimer(time.Millisecond * time.Duration((rand.Intn(50)+50)*multiple))
	defer timer.Stop()
	select {
	case <-s.Closed:
		return
	case <-timer.C:
	}
	select {
	case <-s.semaphore:
	case <-s.Closed:
	}
}

func (s *KeepDialingServer) onDial(conn *Conn) error {
	if s.OnDial != nil {
		return s.OnDial(conn)
	}
	return nil
}

func (s *KeepDialingServer) dial() {
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{RootCAs: s.certPool}}
	netConn, err := dialer.DialContext(s.Context(), "tcp", s.serverAddress)
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

	s.CommonServer.HandleConnection(netConn, keepDialingConnType, func(conn *Conn) error {
		if len(s.authPrivateKeyBytes) != ed25519.PrivateKeySize {
			return errors.New("invalid auth private key size")
		}
		if err := conn.Conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		challenge := make([]byte, 32)
		if _, err := io.ReadFull(conn.Conn, challenge); err != nil {
			return err
		}

		signature := ed25519.Sign(s.authPrivateKeyBytes, challenge)

		var flag byte
		if s.isUpstream {
			flag = 0x01
		} else {
			flag = 0x02
		}

		// inform the relay server our type and group id
		err = WriteAll(conn.Conn, append([]byte{flag, s.groupId}, signature...))
		if err != nil {
			return err
		}

		if err := conn.Conn.SetDeadline(time.Time{}); err != nil {
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
