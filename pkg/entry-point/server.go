package entry_point

import (
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	common "github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type Route struct {
	srcHost string
	srcPort uint16

	dstHost string
	dstPort uint16
}

type EntryPointServer struct {
	*common.KeepDialingServer

	routes []Route
}

func NewEntryPointServer(serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool, _routes []string) *EntryPointServer {
	ks := common.NewKeepDialingServer(false, serverAddress, authPrivateKeyBytes, certPool)

	routes, err := parseRoutes(_routes)
	if err != nil {
		panic(err)
	}

	return &EntryPointServer{
		KeepDialingServer: ks,
		routes:            routes,
	}
}

func parseRoutes(_routes []string) ([]Route, error) {
	routes := make([]Route, len(_routes))

	for _, route := range _routes {
		parts := strings.Split(route, ":")

		if len(parts) <= 1 {
			return nil, errors.New("invalid route")
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

			routes = append(routes, Route{
				srcHost: "*",
				srcPort: uint16(srcPort),
				dstHost: "127.0.0.1",
				dstPort: uint16(dstPort),
			})
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

			routes = append(routes, Route{
				srcHost: srcHost,
				srcPort: uint16(srcPort),
				dstHost: dstHost,
				dstPort: uint16(dstPort),
			})
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

			routes = append(routes, Route{
				srcHost: parts[0],
				srcPort: uint16(srcPort),
				dstHost: parts[2],
				dstPort: uint16(dstPort),
			})
		} else {
			return nil, fmt.Errorf("invalid route: %s", route)
		}
	}

	return routes, nil
}

func (s *EntryPointServer) HandleConnection(conn net.Conn) {
	s.CommonServer.HandleConnection(conn, constant.ConnTypeUp, func(conn *common.Conn) (isUpStream bool, err error) {
		localAddr := conn.Conn.LocalAddr().String()
		host, strPort, err := net.SplitHostPort(localAddr)
		if err != nil {
			return false, err
		}

		uint64Port, err := strconv.ParseUint(strPort, 10, 16)
		if err != nil {
			return false, err
		}

		port := uint16(uint64Port)

		var route *Route
		for _, r := range s.routes {
			if (r.srcHost == "*" || r.srcHost == host) && r.srcPort == port {
				route = &r
				break
			}
		}
		if route == nil {
			return false, fmt.Errorf("no route found for %s:%d", host, port)
		}

		dstHostBytes := net.ParseIP(route.dstHost)
		if len(dstHostBytes) < 16 {
			// fill with 0
			dstHostBytes = append(make([]byte, 16-len(dstHostBytes)), dstHostBytes...)
		}

		dstPostBytes := make([]byte, 16)
		binary.BigEndian.PutUint16(dstPostBytes, route.dstPort)

		_, err = conn.Conn.Write(append(dstHostBytes, dstPostBytes...))
		if err != nil {
			return false, err
		}

		// inform the local server that we are the upstream
		return true, nil
	})
}
