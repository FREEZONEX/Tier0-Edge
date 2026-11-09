package fleettunnel

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// websocketConn 把 WebSocket 二进制消息适配成连续、有序的 net.Conn 字节流，
// 供 yamux 在同一条出站连接上承载多个逻辑连接。
type websocketConn struct {
	conn       *websocket.Conn
	reader     io.Reader
	writeMu    sync.Mutex
	closeOnce  sync.Once
	onActivity func()
}

func newWebsocketConn(conn *websocket.Conn, onActivity func()) *websocketConn {
	return &websocketConn{conn: conn, onActivity: onActivity}
}

func (c *websocketConn) Read(p []byte) (int, error) {
	for {
		if c.reader == nil {
			messageType, reader, err := c.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				continue
			}
			c.reader = reader
		}
		n, err := c.reader.Read(p)
		if n > 0 {
			c.touch()
		}
		if err == io.EOF {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *websocketConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writer, err := c.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, writeErr := writer.Write(p)
	closeErr := writer.Close()
	if writeErr != nil {
		return n, writeErr
	}
	return n, closeErr
}

func (c *websocketConn) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.conn.Close() })
	return err
}

func (c *websocketConn) LocalAddr() net.Addr  { return c.conn.UnderlyingConn().LocalAddr() }
func (c *websocketConn) RemoteAddr() net.Addr { return c.conn.UnderlyingConn().RemoteAddr() }

func (c *websocketConn) SetDeadline(deadline time.Time) error {
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	return c.SetWriteDeadline(deadline)
}

func (c *websocketConn) SetReadDeadline(deadline time.Time) error {
	return c.conn.SetReadDeadline(deadline)
}

func (c *websocketConn) SetWriteDeadline(deadline time.Time) error {
	// Gorilla 将写截止时间也视为写操作，不能与 NextWriter/Close 并发。
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.SetWriteDeadline(deadline)
}

func (c *websocketConn) touch() {
	if c.onActivity != nil {
		c.onActivity()
	}
}
