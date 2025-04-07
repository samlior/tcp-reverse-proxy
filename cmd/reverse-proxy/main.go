package main

import (
	"crypto/x509"
	"fmt"
	"log"
	"os"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	reverse_proxy "github.com/samlior/tcp-reverse-proxy/pkg/reverse-proxy"
	"github.com/spf13/cobra"
)

var (
	BuildTime string
	GitCommit string

	serverCert     *string
	authPrivateKey *string
	serverAddress  *string

	rootCmd = &cobra.Command{
		Use:   "reverse-proxy",
		Short: "Reverse proxy for tcp reverse proxy",
		Long:  "Reverse proxy for tcp reverse proxy",
		Run: func(cmd *cobra.Command, args []string) {
			serverCertBytes, err := os.ReadFile(*serverCert)
			if err != nil {
				log.Fatal("failed to read server certificate:", err)
			}
			authPrivateKeyBytes, err := os.ReadFile(*authPrivateKey)
			if err != nil {
				log.Fatal("failed to read auth private key:", err)
			}

			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(serverCertBytes) {
				log.Fatal("failed to append server certificate to cert pool")
			}

			reverseProxyServer := reverse_proxy.NewReverseProxyServer(*serverAddress, authPrivateKeyBytes, certPool)

			go common.HandleSignal(reverseProxyServer)

			go reverseProxyServer.KeepDialing()

			select {}
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tcp-reverse-proxy/reverse-proxy\n  build time: %s +0\n  git commit: %s\n", BuildTime, GitCommit)
		},
	}
)

func init() {
	serverCert = rootCmd.Flags().StringP("server-cert", "c", "cert/server.crt", "server certificate path")
	authPrivateKey = rootCmd.Flags().StringP("auth-private-key", "p", "cert/auth", "auth private key path")
	serverAddress = rootCmd.Flags().StringP("server-address", "s", "localhost:4433", "server address")

	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
