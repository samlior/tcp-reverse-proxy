package reverse_proxy

import (
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type ReverseProxyServer struct {
	*common.KeepDialingServer
}

func NewReverseProxyServer(serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool) *ReverseProxyServer {
	ks := common.NewKeepDialingServer(true, serverAddress, authPrivateKeyBytes, certPool)

	ks.OnDial = func(conn *common.Conn) error {
		if conn.Type != constant.ConnTypeUp {
			// ignore non-upstream connections
			return nil
		}

		// read the route information
		route := <-conn.Ch
		if len(route) != 16+2 {
			return fmt.Errorf("invalid route: %v, len: %d", route, len(route))
		}

		// set the match id
		conn.MatchId = route

		var dstHost string
		if route[0] != 0 {
			// ipv6
			dstHost = net.IP(route).String()
		} else {
			// ipv4
			dstHost = net.IP(route[12:16]).String()
		}

		dstPort := binary.BigEndian.Uint16(route[16:])

		downConn, err := net.Dial("tcp", dstHost+":"+strconv.Itoa(int(dstPort)))
		if err != nil {
			return err
		}

		go ks.HandleConnection(downConn, constant.ConnTypeDown, func(conn *common.Conn) error {
			// set the match id
			conn.MatchId = route

			return nil
		})

		return nil
	}

	return &ReverseProxyServer{
		KeepDialingServer: ks,
	}
}
