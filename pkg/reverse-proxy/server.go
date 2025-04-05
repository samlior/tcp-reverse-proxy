package reverse_proxy

import (
	"crypto/x509"
	"encoding/binary"
	"errors"
	"net"
	"strconv"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type ReverseProxyServer struct {
	*common.KeepDialingServer
}

func NewReverseProxyServer(serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool) *ReverseProxyServer {
	ks := common.NewKeepDialingServer(false, serverAddress, authPrivateKeyBytes, certPool)

	ks.OnDial = func(conn *common.Conn) error {
		if conn.Type != constant.ConnTypeUp {
			// ignore non-upstream connections
			return nil
		}

		// read the route information
		route := <-conn.Ch
		if len(route) != 16+2 {
			return errors.New("invalid route")
		}

		// set the match id
		conn.MatchId = route

		var dstHost string
		if route[0] != 0 {
			// ipv6
			dstHost = net.IP(route).String()
		} else {
			// ipv4
			dstHost = net.IP(route[13:]).String()
		}

		dstPort := binary.BigEndian.Uint16(route[17:])

		downConn, err := net.Dial("tcp", dstHost+":"+strconv.Itoa(int(dstPort)))
		if err != nil {
			return err
		}

		go ks.HandleConnection(downConn, constant.ConnTypeDown, func(conn *common.Conn) (isUpStream bool, err error) {
			// set the match id
			conn.MatchId = route

			// inform the local server that we are the downstream
			return false, nil
		})

		return nil
	}

	return &ReverseProxyServer{
		KeepDialingServer: ks,
	}
}
