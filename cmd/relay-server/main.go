package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	relay_server "github.com/samlior/tcp-reverse-proxy/pkg/relay-server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	BuildTime string
	GitCommit string

	rootCmd = &cobra.Command{
		Use:   "relay-server",
		Short: "Relay server for tcp reverse proxy",
		Long:  "Relay server for tcp reverse proxy",
		Run: func(cmd *cobra.Command, args []string) {
			serverCert := viper.GetString("serverCert")
			serverKey := viper.GetString("serverKey")
			authPublicKey := viper.GetString("authPublicKey")
			host := viper.GetString("host")
			port := viper.GetInt("port")

			serverCertBytes, err := os.ReadFile(serverCert)
			if err != nil {
				log.Fatal("failed to read server certificate:", err)
			}
			serverKeyBytes, err := os.ReadFile(serverKey)
			if err != nil {
				log.Fatal("failed to read server key:", err)
			}
			authPublicKeyBytes, err := os.ReadFile(authPublicKey)
			if err != nil {
				log.Fatal("failed to read auth public key:", err)
			}

			cert, err := tls.X509KeyPair(serverCertBytes, serverKeyBytes)
			if err != nil {
				log.Fatal("failed to create x509 key pair:", err)
			}

			listener, err := tls.Listen("tcp", fmt.Sprintf("%s:%d", host, port), &tls.Config{
				Certificates: []tls.Certificate{cert},
			})
			if err != nil {
				log.Fatal("failed to listen:", err)
			}

			defer listener.Close()

			log.Printf("listening on %s:%d...", host, port)

			relayServer := relay_server.NewRelayServer(authPublicKeyBytes)

			go common.HandleSignal(relayServer)

			for {
				conn, err := listener.Accept()
				if err != nil {
					log.Println("failed to accept connection:", err)
					continue
				}

				go relayServer.HandleConnection(conn)
			}
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			log.Printf("tcp-reverse-proxy/relay-server\n  build time: %s +0\n  git commit: %s\n", BuildTime, GitCommit)
		},
	}
)

func init() {
	rootCmd.PersistentFlags().String("config", "", "config file (optional, default is CLI only)")

	rootCmd.Flags().StringP("server-cert", "c", "cert/server.crt", "server certificate path")
	rootCmd.Flags().StringP("server-key", "k", "cert/server.key", "server key path")
	rootCmd.Flags().StringP("auth-public-key", "a", "cert/auth.pub", "auth public key path")
	rootCmd.Flags().String("host", "0.0.0.0", "host")
	rootCmd.Flags().IntP("port", "p", 4433, "port")

	rootCmd.AddCommand(versionCmd)

	viper.BindPFlag("serverCert", rootCmd.Flags().Lookup("server-cert"))
	viper.BindPFlag("serverKey", rootCmd.Flags().Lookup("server-key"))
	viper.BindPFlag("authPublicKey", rootCmd.Flags().Lookup("auth-public-key"))
	viper.BindPFlag("host", rootCmd.Flags().Lookup("host"))
	viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))

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
			os.Exit(1)
		}
		log.Println("loaded config file:", viper.ConfigFileUsed())
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("failed to execute root command:", err)
	}
}
