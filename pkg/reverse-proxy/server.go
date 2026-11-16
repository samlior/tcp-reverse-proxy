package reverse_proxy

import (
	"bytes"
	"crypto/x509"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
)

type ReverseProxyServer struct{ *common.KeepDialingServer }

func NewReverseProxyServer(groupId uint8, address string, key []byte, pool *x509.CertPool) *ReverseProxyServer {
	ks := common.NewKeepDialingServer(groupId, true, address, key, pool)
	ks.OnDial = func(c *common.Conn) error {
		// TCP/TLS reads are not message boundaries. Read only the header so that
		// coalesced application bytes remain available for the bridge.
		route := make([]byte, 18)
		if _, err := io.ReadFull(c.Conn, route[:1]); err != nil {
			return err
		}
		if err := c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		if _, err := io.ReadFull(c.Conn, route[1:]); err != nil {
			return err
		}
		if err := c.Conn.SetReadDeadline(time.Time{}); err != nil {
			return err
		}
		ip := net.IP(route[:16])
		if bytes.Equal(route[:12], make([]byte, 12)) {
			ip = net.IP(route[12:16])
		}
		port := binary.BigEndian.Uint16(route[16:])
		dst := net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
		target, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ks.Context(), "tcp", dst)
		if err != nil {
			return err
		}
		c.Target = target
		return nil
	}
	return &ReverseProxyServer{ks}
}
