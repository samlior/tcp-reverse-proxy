package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

type Message struct {
	Flag uint8
	Data []byte
}

func parseMessage(bytes []byte) (*Message, error) {
	if len(bytes) < 2 {
		return nil, fmt.Errorf("invalid message")
	}

	return &Message{Flag: bytes[0], Data: bytes[1:]}, nil
}

type PendingConnection struct {
	conn   net.Conn
	ch     chan []byte
	doneCh chan chan []byte
}

type Connection struct {
	upConn net.Conn
	upCh   chan []byte

	downConn net.Conn
	downCh   chan []byte
}

type RelayServer struct {
	id uint64

	authPublicKeyBytes []byte

	pendingUpConnections   []*PendingConnection
	pendingDownConnections []*PendingConnection

	connections map[uint64]*Connection
}

func NewRelayServer(authPublicKeyBytes []byte) *RelayServer {
	return &RelayServer{
		authPublicKeyBytes: authPublicKeyBytes,
		connections:        make(map[uint64]*Connection),
	}
}

func (rs *RelayServer) readDataFromConn(conn net.Conn, ch chan<- []byte) {
	defer conn.Close()

	buffer := make([]byte, 1024)
	cursor := 0
	length := 0
	left := 0
	pending := []byte{}
	var err error

	read := func(n int) []byte {
		slice := buffer[cursor : cursor+n]
		cursor += n
		length -= n
		return slice
	}

	for {
		length, err = conn.Read(buffer)
		if err == io.EOF {
			// closed
			return
		}
		if err != nil {
			log.Println("error reading from client:", err)
			return
		}

		for length > 0 {
			if left == 0 {
				if length < 2 {
					log.Println("client sent invalid message")
					return
				}

				left = int(binary.BigEndian.Uint16(read(2)))
			}

			var readLength int
			if left > length {
				readLength = length
			} else {
				readLength = left
			}

			pending = append(pending, read(readLength)...)

			left -= readLength

			if left == 0 {
				ch <- pending

				pending = []byte{}
			}
		}

		cursor = 0
		length = 0
	}
}

func (rs *RelayServer) genId() uint64 {
	id := rs.id
	rs.id++
	return id
}

func (rs *RelayServer) registerPendingConn(conn net.Conn, ch chan []byte, isUpStream bool, doneCh chan chan []byte) {
	var pendingConnections *[]*PendingConnection
	var anotherPendingConnections *[]*PendingConnection

	if isUpStream {
		pendingConnections = &rs.pendingUpConnections
		anotherPendingConnections = &rs.pendingDownConnections
	} else {
		pendingConnections = &rs.pendingDownConnections
		anotherPendingConnections = &rs.pendingUpConnections
	}

	if len(*anotherPendingConnections) > 0 {
		another := (*anotherPendingConnections)[0]
		*anotherPendingConnections = (*anotherPendingConnections)[1:]

		id := rs.genId()

		if isUpStream {
			rs.connections[id] = &Connection{
				upConn:   conn,
				upCh:     ch,
				downConn: another.conn,
				downCh:   another.ch,
			}
		} else {
			rs.connections[id] = &Connection{
				upConn:   another.conn,
				upCh:     another.ch,
				downConn: conn,
				downCh:   ch,
			}
		}

		doneCh <- another.ch
		another.doneCh <- ch
	} else {
		*pendingConnections = append(*pendingConnections, &PendingConnection{
			conn,
			ch,
			doneCh,
		})
	}
}

func (rs *RelayServer) HandleConnection(conn net.Conn) {
	log.Println("client connected:", conn.RemoteAddr())

	randomBytes := make([]byte, 32)

	_, err := rand.Read(randomBytes)
	if err != nil {
		log.Println("failed to generate random bytes:", err)
		return
	}

	_, err = conn.Write(randomBytes)
	if err != nil {
		log.Println("error writing to client:", err)
		return
	}

	readCh := make(chan []byte)

	go rs.readDataFromConn(conn, readCh)

	// wait for challenge answer
	select {
	case <-time.After(time.Second):
		log.Println("client challenge timed out")
		return
	case answer := <-readCh:
		// verify challenge answer
		if !ed25519.Verify(rs.authPublicKeyBytes, randomBytes, answer) {
			log.Println("client challenge verification failed")
			return
		}

	}

	doneCh := make(chan chan []byte)

	// TODO: is up stream or down stream?
	rs.registerPendingConn(conn, readCh, true, doneCh)

	writeCh := <-doneCh

	for data := range readCh {
		writeCh <- data
	}
}
