package common

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

func TestCloseInterruptsInitializationAndIsConcurrentSafe(t *testing.T) {
	s := NewCommonServer()
	server, client := net.Pipe()
	defer client.Close()
	entered := make(chan struct{})
	go s.HandleConnection(server, constant.ConnTypeUp, func(c *Conn) error { close(entered); _, err := io.ReadFull(c.Conn, make([]byte, 18)); return err })
	<-entered
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); s.Close() }()
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked during initialization")
	}
}

func TestCloseUnblocksPendingReaderWithFullBuffer(t *testing.T) {
	s := NewCommonServer()
	server, client := net.Pipe()
	defer client.Close()
	entered := make(chan struct{})
	go s.HandleConnection(server, constant.ConnTypeUp, func(c *Conn) error { close(entered); return nil })
	<-entered
	filled := make(chan struct{})
	go func() {
		for i := 0; i < 17; i++ {
			if _, err := client.Write([]byte("banner")); err != nil {
				return
			}
		}
		close(filled)
	}()
	select {
	case <-filled:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not fill its buffer")
	}
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close leaked blocked reader")
	}
}

type shortWriter struct{ data []byte }

func (w *shortWriter) Write(p []byte) (int, error) { w.data = append(w.data, p[0]); return 1, nil }
func TestWriteAllHandlesShortWrites(t *testing.T) {
	w := &shortWriter{}
	if err := WriteAll(w, []byte("header")); err != nil || string(w.data) != "header" {
		t.Fatalf("data=%q err=%v", w.data, err)
	}
}
