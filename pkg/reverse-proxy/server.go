package reverse_proxy

import (
	"crypto/x509"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
)

type ReverseProxyServer struct {
	*common.KeepDialingServer
}

func NewReverseProxyServer(serverAddress string, authPrivateKeyBytes []byte, certPool *x509.CertPool) *ReverseProxyServer {
	ks := common.NewKeepDialingServer(false, serverAddress, authPrivateKeyBytes, certPool)

	return &ReverseProxyServer{
		KeepDialingServer: ks,
	}
}
