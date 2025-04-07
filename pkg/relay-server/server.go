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
		CommonServer:       common.NewCommonServer(),
	}
}

func (s *RelayServer) HandleConnection(conn net.Conn) {
	s.CommonServer.HandleConnection(conn, constant.ConnTypeUnknown, func(conn *common.Conn) error {
		randomBytes := make([]byte, 32)
		_, err := rand.Read(randomBytes)
		if err != nil {
			return err
		}

		_, err = conn.Conn.Write(randomBytes)
		if err != nil {
			return err
		}

		// wait for challenge answer
		select {
		case <-time.After(time.Second):
			return errors.New("client challenge timed out")
		case initialMessage := <-conn.Ch:
			if initialMessage == nil {
				return errors.New("client connection closed")
			}

			if len(initialMessage) != 1+64 {
				return errors.New("client sent invalid initial message")
			}

			signature := initialMessage[1:]

			// set the connection type
			if initialMessage[0] == 1 {
				conn.Type = constant.ConnTypeUp
			} else {
				conn.Type = constant.ConnTypeDown
			}

			// verify challenge signature
			if !ed25519.Verify(s.authPublicKeyBytes, randomBytes, signature) {
				return errors.New("client challenge verification failed")
			}

			return nil
		}
	})
}
