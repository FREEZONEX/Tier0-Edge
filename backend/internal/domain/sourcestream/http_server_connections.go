package sourcestream

import (
	"io"
	"net"
	"sync"
	"time"
)

// Bound sockets before net/http allocates per-connection readers/goroutines.
// The shared budget covers every HTTP Source, including slow idle clients.
type httpBoundedListener struct {
	net.Listener
	slots chan struct{}
}

func (l *httpBoundedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
			return &httpBoundedConn{Conn: conn, slots: l.slots}, nil
		default:
			_ = conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
			_, _ = io.WriteString(conn, "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			_ = conn.Close()
		}
	}
}

type httpBoundedConn struct {
	net.Conn
	slots chan struct{}
	once  sync.Once
}

func (c *httpBoundedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.slots })
	return err
}
