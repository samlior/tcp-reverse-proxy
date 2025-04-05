package common

import (
	"io"
	"log"
	"net"
	"sync"
)

const (
	ConnTypeUp      = "up"
	ConnTypeDown    = "down"
	ConnTypeUnknown = "unknown"
)

type Conn struct {
	id       uint64
	conn     net.Conn
	ch       chan []byte
	ConnType string
}

type PendingConnection struct {
	conn        *Conn
	anotherChCh chan chan []byte
}

type Connection struct {
	up   *Conn
	down *Conn
}

type CommonServer struct {
	Id uint64

	OnPendingConnRemoved func(*Conn)

	PendingUpConnections   []*PendingConnection
	PendingDownConnections []*PendingConnection

	Connections map[uint64]map[uint64]*Connection

	isClosed bool

	lock sync.Mutex
}

func (cs *CommonServer) readDataFromConn(conn *Conn, ch chan<- []byte) {
	defer cs.removeConn(conn)

	buffer := make([]byte, 1024)

	for {
		length, err := conn.conn.Read(buffer)
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

func (cs *CommonServer) writeDataToConn(conn *Conn, ch <-chan []byte) {
	defer cs.removeConn(conn)

	for data := range ch {
		_, err := conn.conn.Write(data)
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

func (cs *CommonServer) registerPendingConn(conn *Conn, isUpStream bool, anotherChCh chan chan []byte) {
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
		cs.onPendingConnRemoved(conn)

		var connection *Connection
		if isUpStream {
			connection = &Connection{
				up:   conn,
				down: another.conn,
			}
		} else {
			connection = &Connection{
				up:   another.conn,
				down: conn,
			}
		}

		cs.Connections[conn.id][another.conn.id] = connection
		cs.Connections[another.conn.id][conn.id] = connection

		anotherChCh <- another.conn.ch
		another.anotherChCh <- conn.ch
	} else {
		// add it to the pending queue
		*pendingConnections = append(*pendingConnections, &PendingConnection{
			conn,
			anotherChCh,
		})
	}
}

func (cs *CommonServer) onPendingConnRemoved(conn *Conn) {
	if cs.OnPendingConnRemoved != nil {
		cs.OnPendingConnRemoved(conn)
	}
}

func (cs *CommonServer) removeConn(conn *Conn) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	log.Println("connection removed:", conn.id, conn.conn.RemoteAddr())

	// remove connected connections
	{
		var anotherId uint64

		for _anotherId, connection := range cs.Connections[conn.id] {
			connection.up.conn.Close()
			connection.down.conn.Close()

			anotherId = _anotherId
		}

		delete(cs.Connections, conn.id)
		delete(cs.Connections, anotherId)
	}

	// remove pending connections
	{
		for i, p := range cs.PendingUpConnections {
			if p.conn == conn {
				p.conn.conn.Close()
				p.anotherChCh <- nil
				cs.PendingUpConnections = append(cs.PendingUpConnections[:i], cs.PendingUpConnections[i+1:]...)
				cs.onPendingConnRemoved(conn)
				return
			}
		}

		for i, p := range cs.PendingDownConnections {
			if p.conn == conn {
				p.conn.conn.Close()
				p.anotherChCh <- nil
				cs.PendingDownConnections = append(cs.PendingDownConnections[:i], cs.PendingDownConnections[i+1:]...)
				cs.onPendingConnRemoved(conn)
				return
			}
		}
	}
}

func (cs *CommonServer) HandleConnection(
	_conn net.Conn,
	onInit func(ch chan []byte) (isUpStream bool, err error),
	connType *string,
) {
	id, closed := cs.genId()
	conn := &Conn{
		id:       id,
		conn:     _conn,
		ch:       make(chan []byte),
		ConnType: ConnTypeUnknown,
	}
	if connType != nil {
		// set the custom conn type if it exists
		conn.ConnType = *connType
	}
	if closed {
		cs.removeConn(conn)
		return
	}

	log.Println("client connected:", id, conn.conn.RemoteAddr())

	readCh := make(chan []byte)

	go cs.readDataFromConn(conn, readCh)

	isUpStream, err := onInit(readCh)
	if err != nil {
		log.Println("error onInit:", err)
		cs.removeConn(conn)
		return
	}

	// set conn type
	if isUpStream {
		conn.ConnType = ConnTypeUp
	} else {
		conn.ConnType = ConnTypeDown
	}

	anotherChCh := make(chan chan []byte, 1)
	cs.registerPendingConn(conn, isUpStream, anotherChCh)
	anotherCh := <-anotherChCh
	if anotherCh == nil {
		cs.removeConn(conn)
		return
	}

	go cs.writeDataToConn(conn, anotherCh)
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
				connection.up.conn.Close()
				connection.down.conn.Close()
			}
		}

		cs.Connections = make(map[uint64]map[uint64]*Connection)
	}

	// clear pending connections
	{
		for _, p := range cs.PendingUpConnections {
			p.anotherChCh <- nil
			p.conn.conn.Close()
		}
		cs.PendingUpConnections = make([]*PendingConnection, 0)

		for _, p := range cs.PendingDownConnections {
			p.anotherChCh <- nil
			p.conn.conn.Close()
		}
		cs.PendingDownConnections = make([]*PendingConnection, 0)
	}
}
