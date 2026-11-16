package reverse_proxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/samlior/tcp-reverse-proxy/pkg/common"
	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

func TestFragmentedRoutePreservesCoalescedPayload(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		c, err := target.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		payload, err := io.ReadAll(c)
		if err == nil {
			common.WriteAll(c, payload)
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	rp := NewReverseProxyServer(0, "", nil, nil)
	// This test enters after relay authentication, without a keep-dialing pool.
	rp.OnConnected = nil
	rp.OnConnClosed = nil
	defer rp.Close()
	go func() {
		c, err := listener.Accept()
		if err == nil {
			rp.CommonServer.HandleConnection(c, constant.ConnTypeUp, rp.OnDial)
		}
	}()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	header := make([]byte, 18)
	copy(header[12:16], net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(header[16:], uint16(target.Addr().(*net.TCPAddr).Port))
	payload := bytes.Repeat([]byte("payload"), 10000)
	// Split the route, then combine its final byte with application data.
	for _, b := range header[:17] {
		if err := common.WriteAll(client, []byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	if err := common.WriteAll(client, append(header[17:], payload...)); err != nil {
		t.Fatal(err)
	}
	client.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(client)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("response length=%d want=%d err=%v", len(got), len(payload), err)
	}
}
