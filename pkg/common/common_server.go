package common

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/samlior/tcp-reverse-proxy/pkg/constant"
)

const PendingTimeout = 2 * time.Minute

type Conn struct {
	Id      uint64
	Conn    net.Conn
	Ch      chan []byte
	Type    string
	Status  int
	GroupId uint8
	Route   []byte
	// Target belongs exclusively to this connection; it is never put in a matching queue.
	Target   net.Conn
	stopRead chan struct{}
	readDone chan struct{}
	readErr  error
}

type PendingConnection struct {
	conn      *Conn
	anotherCh chan *Conn
}

type CommonServer struct {
	Id                     uint64
	OnConnClosed           func(*Conn)
	OnConnected            func(*Conn, *Conn)
	PendingUpConnections   []*PendingConnection
	PendingDownConnections []*PendingConnection
	Closed                 chan struct{}
	lock                   sync.Mutex
	wg                     sync.WaitGroup
	connections            map[uint64]*Conn
	ctx                    context.Context
	cancel                 context.CancelFunc
}

func NewCommonServer() *CommonServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &CommonServer{Id: 1, Closed: make(chan struct{}), connections: make(map[uint64]*Conn), ctx: ctx, cancel: cancel}
}

func (cs *CommonServer) Context() context.Context { return cs.ctx }

func (cs *CommonServer) newConn(n net.Conn, kind string) (*Conn, error) {
	cs.lock.Lock()
	defer cs.lock.Unlock()
	select {
	case <-cs.Closed:
		return nil, net.ErrClosed
	default:
	}
	c := &Conn{Id: cs.Id, Conn: n, Type: kind, Status: constant.ConnStatusPending,
		Ch: make(chan []byte, 16), stopRead: make(chan struct{}), readDone: make(chan struct{})}
	cs.Id++
	cs.connections[c.Id] = c
	// Add and the closed check share the same lock as Close, so Wait cannot race Add.
	cs.wg.Add(1)
	return c, nil
}

func (cs *CommonServer) readData(c *Conn) {
	defer close(c.readDone)
	defer close(c.Ch)
	buf := make([]byte, 32*1024)
	for {
		n, err := c.Conn.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			select {
			case c.Ch <- data:
			case <-c.stopRead:
				return
			case <-cs.Closed:
				return
			}
		}
		if err != nil {
			c.readErr = err
			return
		}
	}
}

func (cs *CommonServer) register(c *Conn, ch chan *Conn) {
	cs.lock.Lock()
	defer cs.lock.Unlock()
	own, other := &cs.PendingDownConnections, &cs.PendingUpConnections
	if c.Type == constant.ConnTypeUp {
		own, other = other, own
	}
	for i, p := range *other {
		if c.GroupId != p.conn.GroupId {
			continue
		}
		*other = append((*other)[:i], (*other)[i+1:]...)
		c.Status = constant.ConnStatusConnected
		p.conn.Status = constant.ConnStatusConnected
		if cs.OnConnected != nil {
			cs.OnConnected(c, p.conn)
		}
		log.Printf("connection connected(%d): %d %s <-> %d %s", c.GroupId, c.Id, c.Conn.RemoteAddr(), p.conn.Id, p.conn.Conn.RemoteAddr())
		ch <- p.conn
		p.anotherCh <- c
		return
	}
	*own = append(*own, &PendingConnection{c, ch})
}

func (cs *CommonServer) remove(c *Conn) {
	cs.lock.Lock()
	for _, queue := range []*[]*PendingConnection{&cs.PendingUpConnections, &cs.PendingDownConnections} {
		for i, p := range *queue {
			if p.conn == c {
				*queue = append((*queue)[:i], (*queue)[i+1:]...)
				break
			}
		}
	}
	if cs.OnConnClosed != nil {
		cs.OnConnClosed(c)
	}
	log.Printf("connection removed(%d): %d %s", c.GroupId, c.Id, c.Conn.RemoteAddr())
	c.Status = constant.ConnStatusClosed
	delete(cs.connections, c.Id)
	cs.lock.Unlock()
	c.Conn.Close()
	if c.Target != nil {
		c.Target.Close()
	}
}

// WriteAll also handles short writes from wrapped connections.
func WriteAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func closeWrite(n net.Conn) error {
	if c, ok := n.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return n.Close()
}

// Bridge preserves TCP half-close: a request ending in EOF can still receive a response.
func Bridge(a, b net.Conn) {
	var wg sync.WaitGroup
	copyTo := func(dst, src net.Conn) {
		defer wg.Done()
		if _, err := io.Copy(dst, src); err != nil {
			a.Close()
			b.Close()
			return
		}
		if err := closeWrite(dst); err != nil {
			a.Close()
			b.Close()
		}
	}
	wg.Add(2)
	go copyTo(a, b)
	go copyTo(b, a)
	wg.Wait()
}

func (cs *CommonServer) HandleConnection(n net.Conn, kind string, init func(*Conn) error) {
	c, err := cs.newConn(n, kind)
	if err != nil {
		n.Close()
		return
	}
	defer cs.wg.Done()
	defer cs.remove(c)
	if err = init(c); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			log.Println("error initializing connection:", err)
		}
		return
	}
	log.Printf("connection initialized(%d): %d %s", c.GroupId, c.Id, c.Conn.RemoteAddr())
	if c.Target != nil {
		log.Printf("connection target(%d): %d -> %s", c.GroupId, c.Id, c.Target.RemoteAddr())
		cs.lock.Lock()
		c.Status = constant.ConnStatusConnected
		if cs.OnConnected != nil {
			cs.OnConnected(c, nil)
		}
		cs.lock.Unlock()
		// Closing the tracked relay socket interrupts both copy directions,
		// including when shutdown happened while the target was being dialed.
		Bridge(c.Conn, c.Target)
		return
	}
	go cs.readData(c)
	// Always join the reader, including when it is blocked sending buffered data.
	defer func() { close(c.stopRead); c.Conn.Close(); <-c.readDone }()
	matched := make(chan *Conn, 1)
	cs.register(c, matched)
	timer := time.NewTimer(PendingTimeout)
	defer timer.Stop()
	readDone := c.readDone
	var peer *Conn
	for peer == nil {
		select {
		case peer = <-matched:
		case <-cs.Closed:
			return
		case <-timer.C:
			return
		case <-readDone:
			// A peer may already have drained Ch before we select its notification.
			select {
			case peer = <-matched:
				continue
			default:
			}
			// Keep client request EOF (even an empty request) until its peer arrives.
			// Empty EOF on an idle relay socket instead means its pool must reconnect.
			if (len(c.Ch) == 0 && len(c.Route) == 0) || (c.readErr != nil && !errors.Is(c.readErr, io.EOF)) {
				return
			}
			readDone = nil
		}
	}
	abort := func() { c.Conn.Close(); peer.Conn.Close() }
	if len(peer.Route) > 0 {
		if err = WriteAll(c.Conn, peer.Route); err != nil {
			abort()
			return
		}
	}
	for {
		select {
		case <-cs.Closed:
			abort()
			return
		case data, ok := <-peer.Ch:
			if !ok {
				// readErr is published before Ch is closed.
				if peer.readErr != nil && !errors.Is(peer.readErr, io.EOF) {
					abort()
					return
				}
				if err = closeWrite(c.Conn); err != nil {
					abort()
					return
				}
				select {
				case <-c.readDone:
				case <-cs.Closed:
					abort()
				}
				return
			}
			if err = WriteAll(c.Conn, data); err != nil {
				abort()
				return
			}
		}
	}
}

func (cs *CommonServer) Close() {
	cs.lock.Lock()
	select {
	case <-cs.Closed:
	default:
		close(cs.Closed)
		cs.cancel()
	}
	conns := make([]net.Conn, 0, len(cs.connections))
	for _, c := range cs.connections {
		conns = append(conns, c.Conn)
	}
	cs.lock.Unlock()
	for _, c := range conns {
		c.Close()
	}
	cs.wg.Wait()
}
