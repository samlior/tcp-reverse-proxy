package common

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"sync"

	constant "github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type Conn struct {
	// unique id
	Id uint64
	// connection
	Conn net.Conn
	// data channel
	Ch chan []byte
	// connection type
	Type string
	// connection status
	Status int

	// match id
	// used to match upstream and downstream in the reverse proxy server
	MatchId []byte
	// route
	// used to store the route information for the entry point server
	// it will be immediately written to the downstream after the connection is established
	Route []byte
}

type PendingConnection struct {
	conn      *Conn
	anotherCh chan *Conn
}

type CommonServer struct {
	Id uint64

	OnConnClosed func(*Conn)
	OnConnected  func(*Conn, *Conn)

	PendingUpConnections   []*PendingConnection
	PendingDownConnections []*PendingConnection

	Closed chan struct{}

	lock sync.Mutex
	wg   sync.WaitGroup

	connections map[uint64]*Conn
}

func NewCommonServer() *CommonServer {
	return &CommonServer{
		Id:                     1,
		PendingUpConnections:   make([]*PendingConnection, 0),
		PendingDownConnections: make([]*PendingConnection, 0),
		connections:            make(map[uint64]*Conn),
		Closed:                 make(chan struct{}),
	}
}

func (cs *CommonServer) readDataFromConn(conn *Conn, readFinished chan struct{}) {
	defer close(conn.Ch)
	defer close(readFinished)

	buffer := make([]byte, 1024)

	for {
		length, err := conn.Conn.Read(buffer)
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			// closed
			return
		}
		if err != nil {
			log.Println("error reading from client:", err)
			return
		}

		// send data to channel
		data := make([]byte, length)
		copy(data, buffer[:length])
		conn.Ch <- data
	}
}

func (cs *CommonServer) writeDataToConn(conn *Conn, ch <-chan []byte, writeFinished chan struct{}) {
	defer close(writeFinished)

	for data := range ch {
		if data == nil {
			log.Println("channel closed")
			return
		}

		_, err := conn.Conn.Write(data)
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			// closed
			return
		}
		if err != nil {
			log.Println("error writing to client:", err)
			return
		}
	}
}

func (cs *CommonServer) registerPendingConn(conn *Conn, anotherCh chan *Conn) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	var pendingConnections *[]*PendingConnection
	var anotherPendingConnections *[]*PendingConnection
	if conn.Type == constant.ConnTypeUp {
		pendingConnections = &cs.PendingUpConnections
		anotherPendingConnections = &cs.PendingDownConnections
	} else {
		pendingConnections = &cs.PendingDownConnections
		anotherPendingConnections = &cs.PendingUpConnections
	}

	if len(*anotherPendingConnections) > 0 {
		// select the matching connection
		var another *PendingConnection
		var anotherIndex int
		for i, p := range *anotherPendingConnections {
			if conn.MatchId == nil || bytes.Equal(p.conn.MatchId, conn.MatchId) {
				another = p
				anotherIndex = i
				break
			}
		}

		if another != nil {
			// remove the connection from the pending queue
			*anotherPendingConnections = append((*anotherPendingConnections)[:anotherIndex], (*anotherPendingConnections)[anotherIndex+1:]...)

			// update status
			conn.Status = constant.ConnStatusConnected
			another.conn.Status = constant.ConnStatusConnected

			// invoke callback
			cs.onConnected(conn, another.conn)

			// return the channel
			anotherCh <- another.conn
			another.anotherCh <- conn

			log.Println("connection connected:", conn.Id, conn.Conn.RemoteAddr(), "<->", another.conn.Id, another.conn.Conn.RemoteAddr())

			return
		}
	}

	// add it to the pending queue
	*pendingConnections = append(*pendingConnections, &PendingConnection{
		conn,
		anotherCh,
	})
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

func (cs *CommonServer) newConn(netConn net.Conn, connType string) (*Conn, error) {
	select {
	case <-cs.Closed:
		return nil, errors.New("server is closed")
	default:
	}

	var id uint64
	{
		cs.lock.Lock()
		defer cs.lock.Unlock()

		id = cs.Id
		cs.Id++
	}

	conn := &Conn{
		Id:     id,
		Conn:   netConn,
		Ch:     make(chan []byte),
		Type:   connType,
		Status: constant.ConnStatusPending,
	}

	// add to connections
	cs.connections[id] = conn

	return conn, nil
}

func (cs *CommonServer) removeConn(conn *Conn) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	if conn.Status == constant.ConnStatusClosed {
		return
	}

	var pendingConnections *[]*PendingConnection
	if conn.Type == constant.ConnTypeUp {
		pendingConnections = &cs.PendingUpConnections
	} else {
		pendingConnections = &cs.PendingDownConnections
	}

	for i, p := range *pendingConnections {
		if p.conn == conn {
			p.anotherCh <- nil
			*pendingConnections = append((*pendingConnections)[:i], (*pendingConnections)[i+1:]...)
			break
		}
	}

	log.Println("connection removed:", conn.Id, conn.Conn.RemoteAddr())

	// invoke callback
	cs.onConnClosed(conn)

	// remove from connections
	delete(cs.connections, conn.Id)

	// update status
	conn.Status = constant.ConnStatusClosed

	conn.Conn.Close()
}

func (cs *CommonServer) HandleConnection(
	netConn net.Conn,
	connType string,
	onInit func(conn *Conn) error,
) {
	conn, err := cs.newConn(netConn, connType)
	if err != nil {
		netConn.Close()
		log.Println("error creating connection:", err)
		return
	}

	cs.wg.Add(1)
	defer cs.wg.Done()

	defer cs.removeConn(conn)

	log.Println("client connected:", conn.Id, conn.Conn.RemoteAddr())

	readFinished := make(chan struct{})
	writeFinished := make(chan struct{})

	go cs.readDataFromConn(conn, readFinished)

	err = onInit(conn)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			return
		}
		log.Println("error initializing connection:", err)
		return
	}

	anotherCh := make(chan *Conn, 1)
	cs.registerPendingConn(conn, anotherCh)

	select {
	case <-cs.Closed:
		return
	case <-readFinished:
		return
	case another := <-anotherCh:
		if another == nil {
			return
		}

		if another.Route != nil {
			_, err := conn.Conn.Write(another.Route)
			if err != nil {
				log.Println("error writing route:", err)
				return
			}

			// clear the route
			another.Route = nil
		}

		go cs.writeDataToConn(conn, another.Ch, writeFinished)
	}

	select {
	case <-cs.Closed:
	case <-readFinished:
	case <-writeFinished:
	}
}

func (cs *CommonServer) Close() {
	close(cs.Closed)

	cs.wg.Wait()
}
