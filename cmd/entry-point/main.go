package main

import (
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	entry_point "github.com/samlior/tcp-reverse-proxy/pkg/entry-point"
	"github.com/spf13/cobra"
)

var (
	BuildTime string
	GitCommit string

	serverCert     *string
	authPrivateKey *string
	serverAddress  *string
	strRoutes      *string

	rootCmd = &cobra.Command{
		Use:   "entry-point",
		Short: "Entry point for tcp reverse proxy",
		Long:  "Entry point for tcp reverse proxy",
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
			ok := certPool.AppendCertsFromPEM(serverCertBytes)
			if !ok {
				log.Fatal("failed to append the server certificate")
			}

			routes, err := entry_point.ParseRoutes(strings.Split(*strRoutes, ","))
			if err != nil {
				log.Fatal("failed to parse routes:", err)
			}

			entryPointServer := entry_point.NewEntryPointServer(*serverAddress, authPrivateKeyBytes, certPool, routes)

			go common.HandleSignal(entryPointServer)

			go entryPointServer.KeepDialing()

			for _, route := range routes {
				srcHost := route.SrcHost
				if srcHost == "*" {
					srcHost = "0.0.0.0"
				}

				listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", srcHost, route.SrcPort))
				if err != nil {
					log.Fatal("failed to listen:", err)
				}

				log.Printf("listening on %s:%d...", srcHost, route.SrcPort)

				for {
					conn, err := listener.Accept()
					if err != nil {
						log.Println("failed to accept connection:", err)
						continue
					}

					go entryPointServer.HandleConnection(conn)
				}
			}
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tcp-reverse-proxy/entry-point\n  build time: %s +0\n  git commit: %s\n", BuildTime, GitCommit)
		},
	}
)

func init() {
	serverCert = rootCmd.Flags().StringP("server-cert", "c", "cert/server.crt", "server certificate path")
	authPrivateKey = rootCmd.Flags().StringP("auth-private-key", "p", "cert/auth", "auth private key path")
	serverAddress = rootCmd.Flags().StringP("server-address", "s", "localhost:4433", "server address")
	strRoutes = rootCmd.Flags().StringP("routes", "r", "", "route addresses, separated by commas")

	rootCmd.MarkFlagRequired("routes")

	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
