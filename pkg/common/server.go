package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type PendingConnection struct {
	id          uint64
	conn        net.Conn
	ch          chan []byte
	anotherChCh chan chan []byte
}

type Connection struct {
	upConn net.Conn
	upCh   chan []byte

	downConn net.Conn
	downCh   chan []byte
}

type RelayServer struct {
	id uint64

	lock sync.Mutex

	authPublicKeyBytes []byte

	pendingUpConnections   []*PendingConnection
	pendingDownConnections []*PendingConnection

	connections map[uint64]map[uint64]*Connection
}

func NewRelayServer(authPublicKeyBytes []byte) *RelayServer {
	return &RelayServer{
		authPublicKeyBytes: authPublicKeyBytes,
		connections:        make(map[uint64]map[uint64]*Connection),
	}
}

func (rs *RelayServer) readDataFromConn(id uint64, conn net.Conn, ch chan<- []byte) {
	defer rs.removeConn(id, conn)

	buffer := make([]byte, 1024)

	for {
		length, err := conn.Read(buffer)
		if err == io.EOF {
			// closed
			return
		}
		if err != nil {
			log.Println("error reading from client:", err)
			return
		}

		// send data to channel
		ch <- buffer[:length]
	}
}

func (rs *RelayServer) writeDataToConn(id uint64, conn net.Conn, ch <-chan []byte) {
	defer rs.removeConn(id, conn)

	for data := range ch {
		_, err := conn.Write(data)
		if err == io.EOF {
			// closed
			return
		}
		if err != nil {
			log.Println("error writing to client:", err)
			return
		}
	}
}

func (rs *RelayServer) genId() uint64 {
	rs.lock.Lock()
	defer rs.lock.Unlock()

	id := rs.id
	rs.id++
	return id
}

func (rs *RelayServer) registerPendingConn(id uint64, conn net.Conn, ch chan []byte, isUpStream bool, anotherChCh chan chan []byte) {
	rs.lock.Lock()
	defer rs.lock.Unlock()

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
		// choose first pending connection
		another := (*anotherPendingConnections)[0]
		*anotherPendingConnections = (*anotherPendingConnections)[1:]

		var connection *Connection
		if isUpStream {
			connection = &Connection{
				upConn:   conn,
				upCh:     ch,
				downConn: another.conn,
				downCh:   another.ch,
			}
		} else {
			connection = &Connection{
				upConn:   another.conn,
				upCh:     another.ch,
				downConn: conn,
				downCh:   ch,
			}
		}

		rs.connections[id][another.id] = connection
		rs.connections[another.id][id] = connection

		anotherChCh <- another.ch
		another.anotherChCh <- ch
	} else {
		// add it to the pending queue
		*pendingConnections = append(*pendingConnections, &PendingConnection{
			id,
			conn,
			ch,
			anotherChCh,
		})
	}
}

func (rs *RelayServer) removeConn(id uint64, conn net.Conn) {
	rs.lock.Lock()
	defer rs.lock.Unlock()

	// remove connected connections
	{
		var anotherId uint64

		for _anotherId, connection := range rs.connections[id] {
			connection.upConn.Close()
			connection.downConn.Close()

			anotherId = _anotherId
		}

		delete(rs.connections, id)
		delete(rs.connections, anotherId)
	}

	// remove pending connections
	{
		for i, p := range rs.pendingUpConnections {
			if p.conn == conn {
				p.anotherChCh <- nil
				rs.pendingUpConnections = append(rs.pendingUpConnections[:i], rs.pendingUpConnections[i+1:]...)
				break
			}
		}

		for i, p := range rs.pendingDownConnections {
			if p.conn == conn {
				p.anotherChCh <- nil
				rs.pendingDownConnections = append(rs.pendingDownConnections[:i], rs.pendingDownConnections[i+1:]...)
				break
			}
		}

		conn.Close()
	}

	log.Println("connection removed:", id, conn.RemoteAddr())
}

func (rs *RelayServer) HandleConnection(conn net.Conn) {
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

	id := rs.genId()
	ch := make(chan []byte)

	log.Println("client connected:", id, conn.RemoteAddr())

	go rs.readDataFromConn(id, conn, ch)

	// wait for challenge answer
	select {
	case <-time.After(time.Second):
		log.Println("client challenge timed out")
		return
	case initialMessage := <-ch:
		if len(initialMessage) != 1+16+2+64 {
			log.Println("client sent invalid initial message")
			return
		}

		isUpStream := initialMessage[0] == 1
		signature := initialMessage[1+16+2 : 1+16+2+64]

		// verify challenge signature
		if !ed25519.Verify(rs.authPublicKeyBytes, randomBytes, signature) {
			log.Println("client challenge verification failed")
			return
		}

		anotherChCh := make(chan chan []byte, 1)

		rs.registerPendingConn(id, conn, ch, isUpStream, anotherChCh)

		anotherCh := <-anotherChCh
		if anotherCh == nil {
			return
		}

		go rs.writeDataToConn(id, conn, anotherCh)
	}
}
