package relay_server

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"time"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type RelayServer struct {
	*common.CommonServer
	authPublicKeyBytes []byte
}

func NewRelayServer(key []byte) *RelayServer {
	return &RelayServer{CommonServer: common.NewCommonServer(), authPublicKeyBytes: key}
}

func (s *RelayServer) HandleConnection(n net.Conn) {
	s.CommonServer.HandleConnection(n, constant.ConnTypeUnknown, func(c *common.Conn) error {
		if len(s.authPublicKeyBytes) != ed25519.PublicKeySize {
			return errors.New("invalid auth public key size")
		}
		// The deadline includes the TLS handshake performed by the first write.
		if err := c.Conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		challenge := make([]byte, 32)
		if _, err := rand.Read(challenge); err != nil {
			return err
		}
		if err := common.WriteAll(c.Conn, challenge); err != nil {
			return err
		}
		// Legacy clients send flag+signature (65 bytes); grouped clients send
		// flag+group+signature (66). Verify the legacy form before reading the extra
		// byte, rather than guessing the format from an arbitrary TCP read size.
		msg := make([]byte, 66)
		if _, err := io.ReadFull(c.Conn, msg[:65]); err != nil {
			return err
		}
		switch msg[0] {
		case 1:
			c.Type = constant.ConnTypeUp
		case 2:
			c.Type = constant.ConnTypeDown
		default:
			return errors.New("invalid connection type")
		}
		if ed25519.Verify(s.authPublicKeyBytes, challenge, msg[1:65]) {
			c.GroupId = 0
		} else {
			if _, err := io.ReadFull(c.Conn, msg[65:]); err != nil {
				return err
			}
			if !ed25519.Verify(s.authPublicKeyBytes, challenge, msg[2:]) {
				return errors.New("client challenge verification failed")
			}
			c.GroupId = msg[1]
		}
		return c.Conn.SetDeadline(time.Time{})
	})
}
