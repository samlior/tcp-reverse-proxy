package entry_point

import (
	"crypto/x509"
	"net"

	common "github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type EntryPointServer struct {
	*common.KeepDialingServer
}

func NewEntryPointServer(serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool) *EntryPointServer {
	ks := common.NewKeepDialingServer(true, serverAddress, authPrivateKeyBytes, certPool)

	return &EntryPointServer{
		KeepDialingServer: ks,
	}
}

func (s *EntryPointServer) HandleConnection(conn net.Conn) {
	s.CommonServer.HandleConnection(conn, constant.ConnTypeUp, func(ch chan []byte) (isUpStream bool, err error) {
		// inform the local server that we are the upstream
		return true, nil
	})
}
