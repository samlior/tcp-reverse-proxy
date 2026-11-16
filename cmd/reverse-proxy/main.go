package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"log"
	"os"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	reverse_proxy "github.com/samlior/tcp-reverse-proxy/pkg/reverse-proxy"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	BuildTime string
	GitCommit string

	rootCmd = &cobra.Command{
		Use:   "reverse-proxy",
		Short: "Reverse proxy for tcp reverse proxy",
		Long:  "Reverse proxy for tcp reverse proxy",
		Run: func(cmd *cobra.Command, args []string) {
			serverCert := viper.GetString("serverCert")
			authPrivateKey := viper.GetString("authPrivateKey")
			serverAddress := viper.GetString("serverAddress")
			group := viper.GetInt("groupId")
			if group < 0 || group > 255 {
				log.Fatal("group id must be between 0 and 255")
			}
			groupId := uint8(group)

			serverCertBytes, err := os.ReadFile(serverCert)
			if err != nil {
				log.Fatal("failed to read server certificate:", err)
			}
			authPrivateKeyBytes, err := os.ReadFile(authPrivateKey)
			if err != nil {
				log.Fatal("failed to read auth private key:", err)
			}

			if len(authPrivateKeyBytes) != ed25519.PrivateKeySize {
				log.Fatal("auth private key must be 64 bytes")
			}

			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(serverCertBytes) {
				log.Fatal("failed to append server certificate to cert pool")
			}

			reverseProxyServer := reverse_proxy.NewReverseProxyServer(groupId, serverAddress, authPrivateKeyBytes, certPool)

			go common.HandleSignal(reverseProxyServer)

			go reverseProxyServer.KeepDialing()

			select {}
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			log.Printf("tcp-reverse-proxy/reverse-proxy\n  build time: %s +0\n  git commit: %s\n", BuildTime, GitCommit)
		},
	}
)

func init() {
	rootCmd.PersistentFlags().String("config", "", "config file (optional, default is CLI only)")

	rootCmd.Flags().StringP("server-cert", "c", "cert/server.crt", "server certificate path")
	rootCmd.Flags().StringP("auth-private-key", "a", "cert/auth", "auth private key path")
	rootCmd.Flags().StringP("server-address", "s", "localhost:4433", "server address")
	rootCmd.Flags().Uint8P("group-id", "g", 0, "group id")

	rootCmd.AddCommand(versionCmd)

	viper.BindPFlag("serverCert", rootCmd.Flags().Lookup("server-cert"))
	viper.BindPFlag("authPrivateKey", rootCmd.Flags().Lookup("auth-private-key"))
	viper.BindPFlag("serverAddress", rootCmd.Flags().Lookup("server-address"))
	viper.BindPFlag("groupId", rootCmd.Flags().Lookup("group-id"))

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
