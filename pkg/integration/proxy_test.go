package integration_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	entry "github.com/samlior/tcp-reverse-proxy/pkg/entry-point"
	relay "github.com/samlior/tcp-reverse-proxy/pkg/relay-server"
	reverse "github.com/samlior/tcp-reverse-proxy/pkg/reverse-proxy"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func serve(l net.Listener, handle func(net.Conn)) {
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()
}

func certificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return cert, pool
}

func TestConcurrentRequestsReconnectAndHalfClose(t *testing.T) {
	cert, pool := certificate(t)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	target := listen(t)
	serve(target, func(c net.Conn) {
		defer c.Close()
		c.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := io.WriteString(c, "SSH-test-banner\n"); err != nil {
			return
		}
		payload, err := io.ReadAll(c)
		if err != nil {
			return
		}
		digest := sha256.Sum256(payload)
		c.Write(digest[:])
	})
	relayListener := tls.NewListener(listen(t), &tls.Config{Certificates: []tls.Certificate{cert}})
	rs := relay.NewRelayServer(pub)
	serve(relayListener, rs.HandleConnection)
	rp := reverse.NewReverseProxyServer(7, relayListener.Addr().String(), key, pool)
	epListener := listen(t)
	routes, err := entry.ParseRoutes([]string{fmt.Sprintf("%d:127.0.0.1:%d", epListener.Addr().(*net.TCPAddr).Port, target.Addr().(*net.TCPAddr).Port)})
	if err != nil {
		t.Fatal(err)
	}
	ep := entry.NewEntryPointServer(7, relayListener.Addr().String(), key, pool, routes)
	serve(epListener, ep.HandleConnection)
	go rp.KeepDialing()
	go ep.KeepDialing()
	t.Cleanup(func() { epListener.Close(); relayListener.Close(); ep.Close(); rp.Close(); rs.Close() })
	// Abandon requests while the destination may already be sending a banner.
	// Subsequent requests must never inherit these connections.
	for i := 0; i < 20; i++ {
		c, err := net.DialTimeout("tcp", epListener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		banner := make([]byte, len("SSH-test-banner\n"))
		if _, err := io.ReadFull(c, banner); err != nil {
			c.Close()
			t.Fatal(err)
		}
		c.Close()
	}
	// A client may half-close an empty request before any pool connection matches.
	empty, err := net.Dial("tcp", epListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	empty.SetDeadline(time.Now().Add(5 * time.Second))
	empty.(*net.TCPConn).CloseWrite()
	response, err := io.ReadAll(empty)
	empty.Close()
	emptyDigest := sha256.Sum256(nil)
	want := append([]byte("SSH-test-banner\n"), emptyDigest[:]...)
	if err != nil || !bytes.Equal(response, want) {
		t.Fatalf("empty request response=%x err=%v", response, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := net.DialTimeout("tcp", epListener.Addr().String(), time.Second)
			if err != nil {
				t.Error(err)
				return
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(15 * time.Second))
			banner := make([]byte, len("SSH-test-banner\n"))
			if _, err := io.ReadFull(c, banner); err != nil {
				t.Errorf("request %d banner: %v", i, err)
				return
			}
			if string(banner) != "SSH-test-banner\n" {
				t.Errorf("unexpected banner %q", banner)
				return
			}
			payload := bytes.Repeat([]byte{byte(i)}, 96*1024+i)
			if _, err := c.Write(payload); err != nil {
				t.Error(err)
				return
			}
			if err := c.(*net.TCPConn).CloseWrite(); err != nil {
				t.Error(err)
				return
			}
			response, err := io.ReadAll(c)
			digest := sha256.Sum256(payload)
			if err != nil || !bytes.Equal(response, digest[:]) {
				t.Errorf("request %d response %x; err=%v", i, response, err)
			}
		}(i)
	}
	wg.Wait()
}
