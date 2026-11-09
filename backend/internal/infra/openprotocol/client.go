package openprotocol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type reply struct {
	frame Frame
	err   error
}
type command struct {
	ctx   context.Context
	mid   int
	data  string
	reply chan reply
}
type Client struct {
	conn       net.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	commands   chan command
	done       chan struct{}
	mu         sync.Mutex
	err        error
	controller Controller
	accept     func(context.Context, Result) error
}

// Dial starts one owner for all reads, writes, device ACKs and heartbeat frames.
// accept must commit reliable local storage before returning nil.
func Dial(ctx context.Context, address string, expected Controller, accept func(context.Context, Result) error) (*Client, error) {
	conn, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	c := &Client{conn: conn, ctx: life, cancel: cancel, commands: make(chan command, 16), done: make(chan struct{}), controller: expected, accept: accept}
	go c.run()
	request, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	f, err := c.request(request, 1, "")
	if err == nil {
		var actual Controller
		actual, err = ParseController(f)
		if err == nil && actual != expected {
			err = errors.New("OpenProtocol.controllerIdentityMismatch")
		}
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Done() <-chan struct{}               { return c.done }
func (c *Client) Err() error                          { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *Client) Close()                              { c.cancel(); c.conn.Close(); <-c.done }
func (c *Client) Subscribe(ctx context.Context) error { _, err := c.request(ctx, 60, ""); return err }
func (c *Client) History(ctx context.Context, id int64) (Result, error) {
	if id < 0 || id > 9999999999 {
		return Result{}, ErrFrame
	}
	f, err := c.request(ctx, 64, fmt.Sprintf("%010d", id))
	if err != nil {
		return Result{}, err
	}
	r, err := ParseResult(f, c.controller)
	if err == nil && id != 0 && r.TighteningID != id {
		return Result{}, ErrFrame
	}
	return r, err
}
func (c *Client) Stop(ctx context.Context) {
	_, _ = c.request(ctx, 63, "")
	_, _ = c.request(ctx, 3, "")
	c.Close()
}
func (c *Client) request(ctx context.Context, mid int, data string) (Frame, error) {
	if _, err := Encode(mid, data); err != nil {
		return Frame{}, err
	}
	req := command{ctx: ctx, mid: mid, data: data, reply: make(chan reply, 1)}
	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-c.done:
		return Frame{}, c.Err()
	case c.commands <- req:
	}
	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case r := <-req.reply:
		return r.frame, r.err
	case <-c.done:
		return Frame{}, c.Err()
	}
}
func (c *Client) write(mid int, data string) error {
	packet, err := Encode(mid, data)
	if err != nil {
		return err
	}
	if err = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	for len(packet) > 0 {
		n, e := c.conn.Write(packet)
		if e != nil {
			return e
		}
		packet = packet[n:]
	}
	return nil
}
func (c *Client) run() {
	var terminal error
	defer func() {
		c.cancel()
		c.conn.Close()
		c.mu.Lock()
		c.err = terminal
		if c.err == nil {
			c.err = context.Canceled
		}
		c.mu.Unlock()
		close(c.done)
	}()
	incoming := make(chan reply, 8)
	go func() {
		r := bufio.NewReaderSize(c.conn, 2048)
		for {
			if err := c.conn.SetReadDeadline(time.Now().Add(14 * time.Second)); err != nil {
				return
			}
			f, err := Read(r)
			select {
			case incoming <- reply{f, err}:
			case <-c.ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	var pending *command
	var deadline <-chan struct{}
	started, subscribed := false, false
	for {
		var ready <-chan command
		if pending == nil {
			ready = c.commands
		}
		select {
		case <-c.ctx.Done():
			terminal = c.ctx.Err()
			return
		case <-deadline:
			terminal = context.DeadlineExceeded
			return
		case <-heartbeat.C:
			if terminal = c.write(9999, ""); terminal != nil {
				return
			}
		case req := <-ready:
			if err := req.ctx.Err(); err != nil {
				req.reply <- reply{err: err}
				continue
			}
			pending = &req
			deadline = req.ctx.Done()
			if terminal = c.write(req.mid, req.data); terminal != nil {
				return
			}
		case message := <-incoming:
			if message.err != nil {
				terminal = message.err
				return
			}
			f := message.frame
			if f.MID == 9999 {
				if f.Data != "" {
					terminal = ErrFrame
					return
				}
				continue
			}
			if f.MID == 61 {
				if !started || !subscribed {
					terminal = ErrFrame
					return
				}
				result, err := ParseResult(f, c.controller)
				if err == nil && c.accept != nil {
					storage, stop := context.WithTimeout(c.ctx, 2*time.Second)
					err = c.accept(storage, result)
					stop()
				} else if err == nil {
					err = errors.New("OpenProtocol.reliableReceiverRequired")
				}
				if err != nil {
					terminal = err
					return
				}
				if terminal = c.write(62, ""); terminal != nil {
					return
				}
				continue
			}
			if pending == nil {
				terminal = ErrFrame
				return
			}
			var err error
			if f.MID == 4 || (pending.mid != 1 && pending.mid != 64) {
				err = CommandReply(f, pending.mid)
			} else if (pending.mid == 1 && f.MID != 2) || (pending.mid == 64 && f.MID != 65) {
				err = ErrFrame
			}
			if errors.Is(err, ErrFrame) {
				terminal = err
				return
			}
			if err == nil {
				switch pending.mid {
				case 1:
					actual, e := ParseController(f)
					if e != nil || actual != c.controller {
						terminal = errors.New("OpenProtocol.controllerIdentityMismatch")
						return
					}
					started = true
				case 60:
					subscribed = true
				case 63:
					subscribed = false
				}
			}
			pending.reply <- reply{frame: f, err: err}
			pending = nil
			deadline = nil
		}
	}
}
