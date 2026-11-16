package entry_point

import (
	"bytes"
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

func NewEntryPointServer(groupId uint8, serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool, routes []Route) *EntryPointServer {
	ks := common.NewKeepDialingServer(groupId, false, serverAddress, authPrivateKeyBytes, certPool)

	return &EntryPointServer{
		KeepDialingServer: ks,
		routes:            routes,
	}
}

// splitRoute permits bracketed IPv6 addresses while preserving the legacy syntax.
func splitRoute(route string) ([]string, error) {
	var parts []string
	start, bracket := 0, false
	for i, r := range route {
		switch r {
		case '[':
			if bracket || i != start {
				return nil, fmt.Errorf("invalid route: %s", route)
			}
			bracket = true
		case ']':
			if !bracket {
				return nil, fmt.Errorf("invalid route: %s", route)
			}
			bracket = false
			if i+1 < len(route) && route[i+1] != ':' {
				return nil, fmt.Errorf("invalid route: %s", route)
			}
		case ':':
			if !bracket {
				parts = append(parts, strings.Trim(route[start:i], "[]"))
				start = i + 1
			}
		}
	}
	if bracket {
		return nil, fmt.Errorf("invalid route: %s", route)
	}
	return append(parts, strings.Trim(route[start:], "[]")), nil
}

func ParseRoutes(input []string) ([]Route, error) {
	routes := make([]Route, 0, len(input))
	seen := make(map[string]bool)
	for _, raw := range input {
		parts, err := splitRoute(raw)
		if err != nil {
			return nil, err
		}
		r := Route{SrcHost: "*", DstHost: "127.0.0.1"}
		var src, dst string
		switch len(parts) {
		case 2:
			src, dst = parts[0], parts[1]
		case 3:
			if _, err := strconv.ParseUint(parts[0], 10, 16); err == nil {
				src, r.DstHost, dst = parts[0], parts[1], parts[2]
			} else {
				r.SrcHost, src, dst = parts[0], parts[1], parts[2]
			}
		case 4:
			r.SrcHost, src, r.DstHost, dst = parts[0], parts[1], parts[2], parts[3]
		default:
			return nil, fmt.Errorf("invalid route: %s", raw)
		}
		sp, err := strconv.ParseUint(src, 10, 16)
		if err != nil || sp == 0 {
			return nil, fmt.Errorf("invalid source port in route: %s", raw)
		}
		dp, err := strconv.ParseUint(dst, 10, 16)
		if err != nil || dp == 0 {
			return nil, fmt.Errorf("invalid destination port in route: %s", raw)
		}
		if r.SrcHost != "*" && net.ParseIP(r.SrcHost) == nil {
			return nil, fmt.Errorf("invalid bind IP in route: %s", raw)
		}
		ip := net.ParseIP(r.DstHost)
		if ip == nil {
			return nil, fmt.Errorf("invalid destination IP in route: %s", raw)
		}
		// The existing wire format reserves twelve zero bytes for legacy IPv4.
		// Reject ambiguous IPv6 destinations instead of silently routing to IPv4.
		if ip.To4() == nil && bytes.Equal(ip.To16()[:12], make([]byte, 12)) {
			return nil, fmt.Errorf("IPv6 destination in ::/96 is ambiguous in the legacy wire format: %s", raw)
		}
		r.SrcPort, r.DstPort = uint16(sp), uint16(dp)
		key := net.JoinHostPort(r.SrcHost, strconv.FormatUint(sp, 10))
		if seen[key] {
			return nil, fmt.Errorf("duplicate listener in route: %s", raw)
		}
		seen[key] = true
		routes = append(routes, r)
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
