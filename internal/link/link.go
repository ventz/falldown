// Package link stands in for the TI graph-link cable: a byte stream between
// two copies of the game, carried over TCP.
//
// The original v1.65 exchanged single bytes in lockstep (brick layout from
// player 1, each ball's x position every frame). The game sends exactly
// those bytes here. The only addition is a "ready" handshake: the host sends
// a ready byte once it has pressed [2nd], and the guest echoes it back, so a
// connection that sat in the kernel's accept backlog (or was abandoned
// there) never starts a game.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// DefaultPort is used when an address has no port.
const DefaultPort = "8383"

const ready byte = 0xFD

// handshakeTimeout bounds how long the host waits for a guest's echo, and
// writeTimeout how long a send may block on a peer that stopped reading.
const (
	handshakeTimeout = 2 * time.Second
	writeTimeout     = 2 * time.Second
)

// Conn is one established link.
type Conn struct {
	c      net.Conn
	recv   chan byte
	done   chan struct{}
	once   sync.Once
	isHost bool
}

// P1 reports whether this side is player 1, the one that generates bricks
// and sends first each frame. The host is always player 1, matching the
// original, where whichever calculator was waiting first became player 1.
func (c *Conn) P1() bool { return c.isHost }

// Recv delivers incoming bytes; it is closed when the peer goes away.
func (c *Conn) Recv() <-chan byte { return c.recv }

// Send writes one byte. A peer that stops reading ends the link rather than
// blocking the game forever.
func (c *Conn) Send(b byte) error {
	c.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err := c.c.Write([]byte{b})
	if err != nil {
		c.Close()
	}
	return err
}

// Close ends the link.
func (c *Conn) Close() {
	c.once.Do(func() {
		close(c.done)
		c.c.Close()
	})
}

func newConn(nc net.Conn, host bool) *Conn {
	if tc, ok := nc.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	c := &Conn{c: nc, recv: make(chan byte, 4096), done: make(chan struct{})}
	c.isHost = host
	go func() {
		defer close(c.recv)
		buf := make([]byte, 256)
		for {
			n, err := nc.Read(buf)
			for _, b := range buf[:n] {
				select {
				case c.recv <- b:
				case <-c.done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return c
}

// Cable knows how to reach the other calculator.
type Cable struct {
	listen  string // host side: address to listen on
	connect string // guest side: address to dial

	mu sync.Mutex
	ln net.Listener // open only while the host is waiting for a guest
}

// NewHost returns a cable that waits for a guest on addr (":8383" style).
func NewHost(addr string) *Cable { return &Cable{listen: withPort(addr)} }

// NewGuest returns a cable that dials a host at addr.
func NewGuest(addr string) *Cable { return &Cable{connect: withPort(addr)} }

// NewPipe returns two connected in-memory links, for tests.
func NewPipe() (p1, p2 *Conn) {
	a, b := net.Pipe()
	return newConn(a, true), newConn(b, false)
}

// Describe says how this cable connects, for the console.
func (cb *Cable) Describe() string {
	if cb.listen != "" {
		return "hosting on " + cb.listen + " (you are player 1)"
	}
	return "joining " + cb.connect + " (you are player 2)"
}

// Establish connects to the other side and completes the ready handshake.
// It blocks until then or until ctx is cancelled.
func (cb *Cable) Establish(ctx context.Context) (*Conn, error) {
	if cb.listen != "" {
		return cb.accept(ctx)
	}
	return cb.dial(ctx)
}

// accept listens only for as long as the host is waiting, so nobody can
// queue up a connection between games.
func (cb *Cable) accept(ctx context.Context) (*Conn, error) {
	cb.mu.Lock()
	l, err := net.Listen("tcp", cb.listen)
	if err != nil {
		cb.mu.Unlock()
		return nil, err
	}
	cb.ln = l
	ln := l.(*net.TCPListener)
	cb.mu.Unlock()
	defer cb.Close()

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ln.SetDeadline(time.Now().Add(200 * time.Millisecond))
		nc, err := ln.Accept()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return nil, err
		}
		if !hostHandshake(nc) {
			nc.Close()
			continue
		}
		return newConn(nc, true), nil
	}
}

func (cb *Cable) dial(ctx context.Context) (*Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	for {
		nc, err := d.DialContext(ctx, "tcp", cb.connect)
		if err == nil {
			c := newConn(nc, false)
			select {
			case b, ok := <-c.recv:
				if ok && b == ready {
					if c.Send(ready) != nil {
						return nil, fmt.Errorf("link: %s went away during the handshake", cb.connect)
					}
					return c, nil
				}
				c.Close()
				if ok {
					return nil, fmt.Errorf("link: unexpected handshake byte %#x from %s", b, cb.connect)
				}
			case <-ctx.Done():
				c.Close()
				return nil, ctx.Err()
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// hostHandshake sends the ready byte and waits briefly for the guest to echo
// it, which weeds out dead or foreign connections.
func hostHandshake(nc net.Conn) bool {
	nc.SetDeadline(time.Now().Add(handshakeTimeout))
	defer nc.SetDeadline(time.Time{})
	if _, err := nc.Write([]byte{ready}); err != nil {
		return false
	}
	var b [1]byte
	_, err := io.ReadFull(nc, b[:])
	return err == nil && b[0] == ready
}

// Close releases the listener, if any.
func (cb *Cable) Close() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.ln != nil {
		cb.ln.Close()
		cb.ln = nil
	}
}

func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, DefaultPort)
}
