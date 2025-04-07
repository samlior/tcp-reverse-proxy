package entry_point

import (
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"

	common "github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type Route struct {
	SrcHost string
	SrcPort uint16

	DstHost string
	DstPort uint16
}

type EntryPointServer struct {
	*common.KeepDialingServer

	routes []Route
}

func NewEntryPointServer(serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool, routes []Route) *EntryPointServer {
	ks := common.NewKeepDialingServer(false, serverAddress, authPrivateKeyBytes, certPool)

	return &EntryPointServer{
		KeepDialingServer: ks,
		routes:            routes,
	}
}

func ParseRoutes(_routes []string) ([]Route, error) {
	routes := make([]Route, len(_routes))

	for i, route := range _routes {
		parts := strings.Split(route, ":")

		if len(parts) <= 1 {
			return nil, fmt.Errorf("invalid route: %s", route)
		} else if len(parts) == 2 {
			// port:port
			srcPort, err := strconv.ParseUint(parts[0], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid source port: %s", parts[0])
			}

			dstPort, err := strconv.ParseUint(parts[1], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid destination port: %s", parts[1])
			}

			routes[i] = Route{
				SrcHost: "*",
				SrcPort: uint16(srcPort),
				DstHost: "127.0.0.1",
				DstPort: uint16(dstPort),
			}
		} else if len(parts) == 3 {
			var srcHost string
			var srcPort uint64
			var dstHost string
			var dstPort uint64
			var err error

			srcPort, err = strconv.ParseUint(parts[0], 10, 16)
			if err != nil {
				// ip:port:port
				srcHost = parts[0]
				srcPort, err = strconv.ParseUint(parts[1], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid source port: %s", parts[0])
				}
				dstHost = "127.0.0.1"
				dstPort, err = strconv.ParseUint(parts[2], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid destination port: %s", parts[1])
				}
			} else {
				// port:ip:port
				srcHost = "*"
				dstHost = parts[1]
				dstPort, err = strconv.ParseUint(parts[2], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid destination port: %s", parts[2])
				}
			}

			routes[i] = Route{
				SrcHost: srcHost,
				SrcPort: uint16(srcPort),
				DstHost: dstHost,
				DstPort: uint16(dstPort),
			}
		} else if len(parts) == 4 {
			// ip:port:ip:port
			srcPort, err := strconv.ParseUint(parts[1], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid source port: %s", parts[1])
			}

			dstPort, err := strconv.ParseUint(parts[3], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid destination port: %s", parts[3])
			}

			routes[i] = Route{
				SrcHost: parts[0],
				SrcPort: uint16(srcPort),
				DstHost: parts[2],
				DstPort: uint16(dstPort),
			}
		} else {
			return nil, fmt.Errorf("invalid route: %s", route)
		}
	}

	return routes, nil
}

func (s *EntryPointServer) HandleConnection(conn net.Conn) {
	s.CommonServer.HandleConnection(conn, constant.ConnTypeUp, func(conn *common.Conn) error {
		localAddr := conn.Conn.LocalAddr().String()
		host, strPort, err := net.SplitHostPort(localAddr)
		if err != nil {
			return err
		}

		uint64Port, err := strconv.ParseUint(strPort, 10, 16)
		if err != nil {
			return err
		}

		port := uint16(uint64Port)

		var route *Route
		for _, r := range s.routes {
			if (r.SrcHost == "*" || r.SrcHost == host) && r.SrcPort == port {
				route = &r
				break
			}
		}
		if route == nil {
			return fmt.Errorf("no route found for %s:%d", host, port)
		}

		dstHostBytes := net.ParseIP(route.DstHost)
		if dstHostBytes.To4() != nil {
			// ipv4
			dstHostBytes = dstHostBytes.To4()
			// fill with 0
			dstHostBytes = append(make([]byte, 16-len(dstHostBytes)), []byte(dstHostBytes)...)
		} else {
			// ipv6
			dstHostBytes = dstHostBytes.To16()
		}

		dstPostBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(dstPostBytes, route.DstPort)

		// set route information
		conn.Route = append([]byte(dstHostBytes), dstPostBytes...)

		return nil
	})
}
