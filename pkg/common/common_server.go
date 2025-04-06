package common

import (
	"bytes"
	"io"
	"log"
	"net"
	"sync"

	constant "github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

type Conn struct {
	Id      uint64
	Conn    net.Conn
	Ch      chan []byte
	Type    string
	MatchId []byte
	Status  int
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

	isClosed bool

	lock sync.Mutex
}

func (cs *CommonServer) readDataFromConn(conn *Conn, readFinished chan struct{}) {
	defer close(conn.Ch)
	defer func() {
		readFinished <- struct{}{}
		close(readFinished)
	}()

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
		data := make([]byte, length)
		copy(data, buffer[:length])
		conn.Ch <- data
	}
}

func (cs *CommonServer) writeDataToConn(conn *Conn, ch <-chan []byte, writeFinished chan struct{}) {
	defer func() {
		writeFinished <- struct{}{}
		close(writeFinished)
	}()

	for data := range ch {
		if data == nil {
			log.Println("channel closed")
			return
		}

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
			p.conn.Conn.Close()
			p.anotherCh <- nil
			*pendingConnections = append((*pendingConnections)[:i], (*pendingConnections)[i+1:]...)
			break
		}
	}

	log.Println("connection removed:", conn.Id, conn.Conn.RemoteAddr())

	// update status
	conn.Status = constant.ConnStatusClosed

	// invoke callback
	cs.onConnClosed(conn)
}

func (cs *CommonServer) HandleConnection(
	netConn net.Conn,
	connType string,
	onInit func(conn *Conn) error,
) {
	id, closed := cs.genId()
	conn := &Conn{
		Id:     id,
		Conn:   netConn,
		Ch:     make(chan []byte),
		Type:   connType,
		Status: constant.ConnStatusPending,
	}
	defer cs.removeConn(conn)
	if closed {
		return
	}

	log.Println("client connected:", id, conn.Conn.RemoteAddr())

	readFinished := make(chan struct{})
	writeFinished := make(chan struct{})

	go cs.readDataFromConn(conn, readFinished)

	err := onInit(conn)
	if err != nil {
		log.Println("error onInit:", err)
		return
	}

	anotherCh := make(chan *Conn, 1)
	cs.registerPendingConn(conn, anotherCh)

	select {
	case <-readFinished:
		return
	case another := <-anotherCh:
		if another == nil {
			return
		}

		go cs.writeDataToConn(conn, another.Ch, writeFinished)
	}

	<-readFinished

	// manually remove the connection again
	cs.removeConn(conn)

	<-writeFinished
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

	for _, p := range cs.PendingUpConnections {
		p.anotherCh <- nil
		p.conn.Conn.Close()
	}
	cs.PendingUpConnections = make([]*PendingConnection, 0)

	for _, p := range cs.PendingDownConnections {
		p.anotherCh <- nil
		p.conn.Conn.Close()
	}
	cs.PendingDownConnections = make([]*PendingConnection, 0)
}
