package common

import (
	"log"
	"os"
	"os/signal"
	"time"
)

type CloseableServer interface {
	Close()
}

func HandleSignal(server CloseableServer) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)

	<-signalCh

	log.Println("received interrupt signal, shutting down...")

	select {
	case <-func() <-chan struct{} {
		ch := make(chan struct{})
		go func() {
			server.Close()
			close(ch)
		}()
		return ch
	}():
		log.Println("server has been shut down")
		os.Exit(0)
	case <-time.After(time.Second * 3):
		log.Println("server shutdown timeout")
		os.Exit(1)
	}
}
