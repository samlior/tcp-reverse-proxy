package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Short: "Generate a x509 or ed25519 key pair",
		Long:  "Generate a x509 or ed25519 key pair",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	x509Cmd = &cobra.Command{
		Use:   "x509",
		Short: "Generate a x509 key pair",
		Long:  "Generate a x509 key pair",
		Run: func(cmd *cobra.Command, args []string) {
			output, _ := cmd.Flags().GetString("output")
			dns, _ := cmd.Flags().GetStringSlice("dns")
			ip, _ := cmd.Flags().GetStringSlice("ip")

			// 1. Create output directory if it doesn't exist
			createOutputDirIfNotExists(output)

			// 2. Generate RSA private key
			privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				log.Fatalf("failed to generate private key: %v", err)
			}

			ipAddresses := make([]net.IP, len(ip))
			for i, ipStr := range ip {
				ipAddresses[i] = net.ParseIP(ipStr)
			}

			// 3. Create certificate template
			serialNumber, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
			template := x509.Certificate{
				SerialNumber:          serialNumber,
				Subject:               pkix.Name{},
				NotBefore:             time.Now(),
				NotAfter:              time.Now().Add(365 * 24 * time.Hour), // valid for 1 year
				KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				BasicConstraintsValid: true,
				IsCA:                  true, // set to true if acting as a CA

				// Subject Alternative Name (SAN)
				DNSNames:    dns,
				IPAddresses: ipAddresses,
			}

			// 4. Create self-signed certificate
			derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
			if err != nil {
				log.Fatalf("failed to create certificate: %v", err)
			}

			// 5. Save certificate to cert.pem
			certOut, err := os.Create(filepath.Join(output, "server.crt"))
			if err != nil {
				log.Fatalf("failed to create server.crt: %v", err)
			}
			pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
			certOut.Close()
			fmt.Println("certificate saved to", filepath.Join(output, "server.crt"))

			// 6. Save private key to key.pem
			keyOut, err := os.Create(filepath.Join(output, "server.key"))
			if err != nil {
				log.Fatalf("failed to create server.key: %v", err)
			}
			pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
			keyOut.Close()
			fmt.Println("private key saved to", filepath.Join(output, "server.key"))
		},
	}

	ed25519Cmd = &cobra.Command{
		Use:   "ed25519",
		Short: "Generate a ed25519 key pair",
		Long:  "Generate a ed25519 key pair",
		Run: func(cmd *cobra.Command, args []string) {
			output, _ := cmd.Flags().GetString("output")

			// 1. Create output directory if it doesn't exist
			createOutputDirIfNotExists(output)

			// 2. Generate Ed25519 key pair
			publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				log.Fatalf("failed to generate ed25519 key pair: %v", err)
			}

			// 3. Save private key to auth
			privateKeyFile, err := os.Create(filepath.Join(output, "auth"))
			if err != nil {
				log.Fatalf("failed to create auth: %v", err)
			}
			if err := os.WriteFile(filepath.Join(output, "auth"), privateKey, 0644); err != nil {
				log.Fatalf("failed to write auth: %v", err)
			}
			privateKeyFile.Close()
			fmt.Println("private key saved to", filepath.Join(output, "auth"))

			// 4. Save public key to auth.pub
			publicKeyFile, err := os.Create(filepath.Join(output, "auth.pub"))
			if err != nil {
				log.Fatalf("failed to create auth.pub: %v", err)
			}
			if err := os.WriteFile(filepath.Join(output, "auth.pub"), publicKey, 0644); err != nil {
				log.Fatalf("failed to write auth.pub: %v", err)
			}
			publicKeyFile.Close()
			fmt.Println("public key saved to", filepath.Join(output, "auth.pub"))
		},
	}
)

func createOutputDirIfNotExists(output string) {
	stat, err := os.Stat(output)
	if os.IsNotExist(err) {
		err = os.MkdirAll(output, 0755)
		if err != nil {
			log.Fatalf("failed to create output directory: %v", err)
		}
	} else if err != nil {
		log.Fatalf("failed to check output directory: %v", err)
	} else if !stat.IsDir() {
		log.Fatalf("output is not a directory: %v", output)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("output", "o", "cert", "output directory")

	x509Cmd.Flags().StringSliceP("dns", "d", []string{}, "DNS name separated by commas")
	x509Cmd.Flags().StringSliceP("ip", "i", []string{}, "IP address separated by commas")

	rootCmd.AddCommand(x509Cmd)
	rootCmd.AddCommand(ed25519Cmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("failed to execute root command:", err)
	}
}
