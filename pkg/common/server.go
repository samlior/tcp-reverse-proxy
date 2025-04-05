package common

import (
	"io"
	"log"
	"net"
	"sync"

	constant "github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type Conn struct {
	Id   uint64
	Conn net.Conn
	Ch   chan []byte
	Type string
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

	OnConnClosed func(*Conn)
	OnConnected  func(*Conn, *Conn)

	PendingUpConnections   []*PendingConnection
	PendingDownConnections []*PendingConnection

	Connections map[uint64]map[uint64]*Connection

	isClosed bool

	lock sync.Mutex
}

func (cs *CommonServer) readDataFromConn(conn *Conn) {
	defer cs.removeConn(conn)

	buffer := make([]byte, 1024)

	for {
		length, err := conn.Conn.Read(buffer)
		if err == io.EOF {
			// closed
			return
		}
		if err != nil {
			log.Println("error reading from client:", err)
			return
		}

		// send data to channel
		conn.Ch <- buffer[:length]
	}
}

func (cs *CommonServer) writeDataToConn(conn *Conn, ch <-chan []byte) {
	defer cs.removeConn(conn)

	for data := range ch {
		_, err := conn.Conn.Write(data)
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

		// invoke callback
		cs.onConnected(conn, another.conn)

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

		// add connection to map
		cs.Connections[conn.Id][another.conn.Id] = connection
		cs.Connections[another.conn.Id][conn.Id] = connection

		// return the channel
		anotherChCh <- another.conn.Ch
		another.anotherChCh <- conn.Ch
	} else {
		// add it to the pending queue
		*pendingConnections = append(*pendingConnections, &PendingConnection{
			conn,
			anotherChCh,
		})
	}
}

func (cs *CommonServer) onConnClosed(conn *Conn) {
	if cs.OnConnClosed != nil {
		cs.OnConnClosed(conn)
	}
}

func (cs *CommonServer) onConnected(conn *Conn, anotherConn *Conn) {
	if cs.OnConnected != nil {
		cs.OnConnected(conn, anotherConn)
	}
}

func (cs *CommonServer) removeConn(conn *Conn) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	log.Println("connection removed:", conn.Id, conn.Conn.RemoteAddr())

	// remove connected connections
	{
		var anotherId uint64

		for _anotherId, connection := range cs.Connections[conn.Id] {
			connection.up.Conn.Close()
			connection.down.Conn.Close()

			anotherId = _anotherId

			cs.onConnClosed(connection.up)
			cs.onConnClosed(connection.down)
		}

		delete(cs.Connections, conn.Id)
		delete(cs.Connections, anotherId)
	}

	// remove pending connections
	{
		for i, p := range cs.PendingUpConnections {
			if p.conn == conn {
				p.conn.Conn.Close()
				p.anotherChCh <- nil
				cs.PendingUpConnections = append(cs.PendingUpConnections[:i], cs.PendingUpConnections[i+1:]...)
				cs.onConnClosed(conn)
				return
			}
		}

		for i, p := range cs.PendingDownConnections {
			if p.conn == conn {
				p.conn.Conn.Close()
				p.anotherChCh <- nil
				cs.PendingDownConnections = append(cs.PendingDownConnections[:i], cs.PendingDownConnections[i+1:]...)
				cs.onConnClosed(conn)
				return
			}
		}
	}
}

func (cs *CommonServer) HandleConnection(
	_conn net.Conn,
	connType string,
	onInit func(conn *Conn) (isUpStream bool, err error),
) {
	id, closed := cs.genId()
	conn := &Conn{
		Id:   id,
		Conn: _conn,
		Ch:   make(chan []byte),
		Type: connType,
	}
	if closed {
		cs.removeConn(conn)
		return
	}

	log.Println("client connected:", id, conn.Conn.RemoteAddr())

	go cs.readDataFromConn(conn)

	isUpStream, err := onInit(conn)
	if err != nil {
		log.Println("error onInit:", err)
		cs.removeConn(conn)
		return
	}

	// set conn type
	if isUpStream {
		conn.Type = constant.ConnTypeUp
	} else {
		conn.Type = constant.ConnTypeDown
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
				connection.up.Conn.Close()
				connection.down.Conn.Close()
			}
		}

		cs.Connections = make(map[uint64]map[uint64]*Connection)
	}

	// clear pending connections
	{
		for _, p := range cs.PendingUpConnections {
			p.anotherChCh <- nil
			p.conn.Conn.Close()
		}
		cs.PendingUpConnections = make([]*PendingConnection, 0)

		for _, p := range cs.PendingDownConnections {
			p.anotherChCh <- nil
			p.conn.Conn.Close()
		}
		cs.PendingDownConnections = make([]*PendingConnection, 0)
	}
}
