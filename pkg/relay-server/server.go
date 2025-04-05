package relay_server

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"time"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type RelayServer struct {
	*common.CommonServer

	authPublicKeyBytes []byte
}

func NewRelayServer(authPublicKeyBytes []byte) *RelayServer {
	return &RelayServer{
		authPublicKeyBytes: authPublicKeyBytes,
		CommonServer: &common.CommonServer{
			Id:                     1,
			PendingUpConnections:   make([]*common.PendingConnection, 0),
			PendingDownConnections: make([]*common.PendingConnection, 0),
			Connections:            make(map[uint64]map[uint64]*common.Connection),
		},
	}
}

func (s *RelayServer) HandleConnection(conn net.Conn) {
	s.CommonServer.HandleConnection(conn, constant.ConnTypeUnknown, func(conn *common.Conn) (isUpStream bool, err error) {
		randomBytes := make([]byte, 32)
		_, err = rand.Read(randomBytes)
		if err != nil {
			return
		}

		_, err = conn.Conn.Write(randomBytes)
		if err != nil {
			return
		}

		// wait for challenge answer
		select {
		case <-time.After(time.Second):
			err = errors.New("client challenge timed out")
			return
		case initialMessage := <-conn.Ch:
			if len(initialMessage) != 1+64 {
				err = errors.New("client sent invalid initial message")
				return
			}

			isUpStream = initialMessage[0] == 1
			signature := initialMessage[1:]

			// verify challenge signature
			if !ed25519.Verify(s.authPublicKeyBytes, randomBytes, signature) {
				err = errors.New("client challenge verification failed")
				return
			}

			return
		}
	})
}
