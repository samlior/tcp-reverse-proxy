package common

import (
	"io"
	"log"
	"net"
	"sync"
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

type CommonServer struct {
	Id uint64

	OnPendingConnRemoved func(id uint64, conn net.Conn, isUpStream bool)

	PendingUpConnections   []*PendingConnection
	PendingDownConnections []*PendingConnection

	Connections map[uint64]map[uint64]*Connection

	isClosed bool

	lock sync.Mutex
}

func (cs *CommonServer) readDataFromConn(id uint64, conn net.Conn, ch chan<- []byte) {
	defer cs.removeConn(id, conn)

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

func (cs *CommonServer) writeDataToConn(id uint64, conn net.Conn, ch <-chan []byte) {
	defer cs.removeConn(id, conn)

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

func (cs *CommonServer) genId() (uint64, bool) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	id := cs.Id
	cs.Id++
	return id, cs.isClosed
}

func (cs *CommonServer) registerPendingConn(id uint64, conn net.Conn, ch chan []byte, isUpStream bool, anotherChCh chan chan []byte) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	var pendingConnections *[]*PendingConnection
	var anotherPendingConnections *[]*PendingConnection

	if isUpStream {
		pendingConnections = &cs.PendingUpConnections
		anotherPendingConnections = &cs.PendingDownConnections
	} else {
		pendingConnections = &cs.PendingDownConnections
		anotherPendingConnections = &cs.PendingUpConnections
	}

	if len(*anotherPendingConnections) > 0 {
		// choose first pending connection
		another := (*anotherPendingConnections)[0]
		*anotherPendingConnections = (*anotherPendingConnections)[1:]
		cs.onPendingConnRemoved(another.id, another.conn, !isUpStream)

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

		cs.Connections[id][another.id] = connection
		cs.Connections[another.id][id] = connection

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

func (cs *CommonServer) onPendingConnRemoved(id uint64, conn net.Conn, isUpStream bool) {
	if cs.OnPendingConnRemoved != nil {
		cs.OnPendingConnRemoved(id, conn, isUpStream)
	}
}

func (cs *CommonServer) removeConn(id uint64, conn net.Conn) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	log.Println("connection removed:", id, conn.RemoteAddr())

	// remove connected connections
	{
		var anotherId uint64

		for _anotherId, connection := range cs.Connections[id] {
			connection.upConn.Close()
			connection.downConn.Close()

			anotherId = _anotherId
		}

		delete(cs.Connections, id)
		delete(cs.Connections, anotherId)
	}

	// remove pending connections
	{
		for i, p := range cs.PendingUpConnections {
			if p.conn == conn {
				p.conn.Close()
				p.anotherChCh <- nil
				cs.PendingUpConnections = append(cs.PendingUpConnections[:i], cs.PendingUpConnections[i+1:]...)
				cs.onPendingConnRemoved(p.id, p.conn, true)
				return
			}
		}

		for i, p := range cs.PendingDownConnections {
			if p.conn == conn {
				p.conn.Close()
				p.anotherChCh <- nil
				cs.PendingDownConnections = append(cs.PendingDownConnections[:i], cs.PendingDownConnections[i+1:]...)
				cs.onPendingConnRemoved(p.id, p.conn, false)
				return
			}
		}
	}
}

func (cs *CommonServer) HandleConnection(
	conn net.Conn,
	onInit func(ch chan []byte) (isUpStream bool, err error),
) {
	id, closed := cs.genId()
	if closed {
		conn.Close()
		return
	}

	readCh := make(chan []byte)

	log.Println("client connected:", id, conn.RemoteAddr())

	go cs.readDataFromConn(id, conn, readCh)

	isUpStream, err := onInit(readCh)
	if err != nil {
		log.Println("error onInit:", err)
		return
	}

	anotherChCh := make(chan chan []byte, 1)
	cs.registerPendingConn(id, conn, readCh, isUpStream, anotherChCh)
	anotherCh := <-anotherChCh
	if anotherCh == nil {
		return
	}

	go cs.writeDataToConn(id, conn, anotherCh)
}

func (cs *CommonServer) IsClosed() bool {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	return cs.isClosed
}

func (cs *CommonServer) Close() {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	if cs.isClosed {
		return
	}

	cs.isClosed = true

	// clear connected connections
	{
		for _, _map := range cs.Connections {
			for _, connection := range _map {
				connection.upConn.Close()
				connection.downConn.Close()
			}
		}

		cs.Connections = make(map[uint64]map[uint64]*Connection)
	}

	// clear pending connections
	{
		for _, p := range cs.PendingUpConnections {
			p.anotherChCh <- nil
			p.conn.Close()
		}
		cs.PendingUpConnections = make([]*PendingConnection, 0)

		for _, p := range cs.PendingDownConnections {
			p.anotherChCh <- nil
			p.conn.Close()
		}
		cs.PendingDownConnections = make([]*PendingConnection, 0)
	}
}
