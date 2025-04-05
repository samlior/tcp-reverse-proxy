package entry_point

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"math/rand"
	"net"
	"time"

	common "github.com/samlior/tcp-reverse-proxy/pkg/common"
	constant "github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type EntryPointServer struct {
	common.CommonServer

	semaphore           chan struct{}
	certPool            *x509.CertPool
	serverAddress       string
	authPrivateKeyBytes []byte
}

func NewEntryPointServer(serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool) *EntryPointServer {
	s := &EntryPointServer{
		semaphore:           make(chan struct{}, constant.Concurrency),
		serverAddress:       serverAddress,
		authPrivateKeyBytes: authPrivateKeyBytes,
		certPool:            certPool,
		CommonServer: common.CommonServer{
			Id:                     1,
			PendingUpConnections:   make([]*common.PendingConnection, 0),
			PendingDownConnections: make([]*common.PendingConnection, 0),
			Connections:            make(map[uint64]map[uint64]*common.Connection),
		},
	}

	s.OnConnClosed = func(conn *common.Conn) {
		// whenever a downstream connection drops,
		// immediately establish a new one to maintain
		// a consistent number of pending downstream connections
		if (conn.Type == constant.ConnTypeDown || conn.Type == constant.ConnTypePendingDown) && !s.CommonServer.IsClosed() {
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)+50))
			<-s.semaphore
		}
	}

	s.OnConnected = func(conn *common.Conn, anotherConn *common.Conn) {
		time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)+50))
		<-s.semaphore
	}

	go s.keepDialing()

	return s
}

func (s *EntryPointServer) dial() {
	conn, err := tls.Dial("tcp", s.serverAddress, &tls.Config{
		RootCAs: s.certPool,
	})
	if err != nil {
		log.Println("failed to dial to relay server:", err)
		return
	}

	s.CommonServer.HandleConnection(conn, func(ch chan []byte) (isUpStream bool, err error) {
		challenge := <-ch
		if len(challenge) != 32 {
			return false, errors.New("invalid challenge")
		}

		signature := ed25519.Sign(s.authPrivateKeyBytes, challenge)

		// this is the entry point
		// inform the relay server that we are the upstream
		_, err = conn.Write(append([]byte{0x01}, signature...))
		if err != nil {
			return false, err
		}

		// inform the local server that we are the downstream
		return false, nil
	}, &constant.ConnTypePendingDown)
}

func (s *EntryPointServer) keepDialing() {
	for !s.CommonServer.IsClosed() {
		s.semaphore <- struct{}{}
		go s.dial()
	}
}

func (s *EntryPointServer) HandleConnection(conn net.Conn) {
	s.CommonServer.HandleConnection(conn, func(ch chan []byte) (isUpStream bool, err error) {
		// inform the local server that we are the upstream
		return true, nil
	}, nil)
}
