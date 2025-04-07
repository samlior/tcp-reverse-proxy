package main

import (
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	entry_point "github.com/samlior/tcp-reverse-proxy/pkg/entry-point"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	BuildTime string
	GitCommit string

	rootCmd = &cobra.Command{
		Use:   "entry-point",
		Short: "Entry point for tcp reverse proxy",
		Long:  "Entry point for tcp reverse proxy",
		Run: func(cmd *cobra.Command, args []string) {
			serverCert := viper.GetString("serverCert")
			authPrivateKey := viper.GetString("authPrivateKey")
			serverAddress := viper.GetString("serverAddress")
			_routes := viper.GetStringSlice("routes")

			if len(_routes) == 0 {
				log.Fatal("routes is required")
			}

			serverCertBytes, err := os.ReadFile(serverCert)
			if err != nil {
				log.Fatal("failed to read server certificate:", err)
			}
			authPrivateKeyBytes, err := os.ReadFile(authPrivateKey)
			if err != nil {
				log.Fatal("failed to read auth private key:", err)
			}

			certPool := x509.NewCertPool()
			ok := certPool.AppendCertsFromPEM(serverCertBytes)
			if !ok {
				log.Fatal("failed to append the server certificate")
			}

			routes, err := entry_point.ParseRoutes(_routes)
			if err != nil {
				log.Fatal("failed to parse routes:", err)
			}

			entryPointServer := entry_point.NewEntryPointServer(serverAddress, authPrivateKeyBytes, certPool, routes)

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
			log.Printf("tcp-reverse-proxy/entry-point\n  build time: %s +0\n  git commit: %s\n", BuildTime, GitCommit)
		},
	}
)

func init() {
	rootCmd.PersistentFlags().String("config", "", "config file (optional, default is CLI only)")

	rootCmd.Flags().StringP("server-cert", "c", "cert/server.crt", "server certificate path")
	rootCmd.Flags().StringP("auth-private-key", "a", "cert/auth", "auth private key path")
	rootCmd.Flags().StringP("server-address", "s", "localhost:4433", "server address")
	rootCmd.Flags().StringSliceP("routes", "r", []string{}, "route addresses, separated by commas")

	rootCmd.AddCommand(versionCmd)

	viper.BindPFlag("serverCert", rootCmd.Flags().Lookup("server-cert"))
	viper.BindPFlag("authPrivateKey", rootCmd.Flags().Lookup("auth-private-key"))
	viper.BindPFlag("serverAddress", rootCmd.Flags().Lookup("server-address"))
	viper.BindPFlag("routes", rootCmd.Flags().Lookup("routes"))

	viper.AutomaticEnv()

	cobra.OnInitialize(initConfig)
}

func initConfig() {
	cfgFile, _ := rootCmd.Flags().GetString("config")
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		err := viper.ReadInConfig()
		if err != nil {
			log.Fatal("failed to read config file:", err)
		}
		log.Println("loaded config file:", viper.ConfigFileUsed())
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("failed to execute root command:", err)
	}
}
