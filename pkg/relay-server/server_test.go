package relay_server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"
)

func authenticate(t *testing.T, s *RelayServer, key ed25519.PrivateKey, flag, group byte, legacy, fragment bool, payload []byte) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { client.Close() })
	go s.HandleConnection(server)
	client.SetDeadline(time.Now().Add(3 * time.Second))
	challenge := make([]byte, 32)
	if _, err := io.ReadFull(client, challenge); err != nil {
		t.Fatal(err)
	}
	header := []byte{flag}
	if !legacy {
		header = append(header, group)
	}
	msg := append(header, ed25519.Sign(key, challenge)...)
	if fragment {
		for _, b := range msg[:len(msg)-1] {
			if _, err := client.Write([]byte{b}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := client.Write(append(msg[len(msg)-1:], payload...)); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := client.Write(append(msg, payload...)); err != nil {
			t.Fatal(err)
		}
	}
	return client
}

func TestHandshakeFragmentationCoalescingAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, fragment := range []bool{false, true} {
			name := "grouped"
			if legacy {
				name = "legacy"
			}
			if fragment {
				name += "/fragmented"
			} else {
				name += "/coalesced"
			}
			t.Run(name, func(t *testing.T) {
				pub, key, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				s := NewRelayServer(pub)
				defer s.Close()
				payload := []byte("application payload immediately after authentication")
				up := authenticate(t, s, key, 1, 7, legacy, fragment, payload)
				down := authenticate(t, s, key, 2, 7, legacy, fragment, nil)
				got := make([]byte, len(payload))
				if _, err := io.ReadFull(down, got); err != nil || !bytes.Equal(got, payload) {
					t.Fatalf("got %q err=%v", got, err)
				}
				up.Close()
				down.Close()
			})
		}
	}
}

func TestGroupsDoNotMatch(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	s := NewRelayServer(pub)
	defer s.Close()
	up := authenticate(t, s, key, 1, 7, false, false, []byte("group7"))
	wrong := authenticate(t, s, key, 2, 8, false, false, nil)
	wrong.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := wrong.Read(make([]byte, 6)); err == nil {
		t.Fatal("different groups matched")
	}
	right := authenticate(t, s, key, 2, 7, false, false, nil)
	got := make([]byte, 6)
	if _, err := io.ReadFull(right, got); err != nil || string(got) != "group7" {
		t.Fatalf("got %q err=%v", got, err)
	}
	up.Close()
	wrong.Close()
	right.Close()
}

func TestInvalidRoleRejected(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	s := NewRelayServer(pub)
	defer s.Close()
	client := authenticate(t, s, key, 3, 0, true, false, nil)
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("invalid connection role accepted")
	}
}
