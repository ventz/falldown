package link

import (
	"context"
	"net"
	"testing"
	"time"
)

// A host and a guest over real TCP: the guest may dial first and keep
// retrying, and both must see each other's bytes in order.
func TestHostGuestOverTCP(t *testing.T) {
	host := NewHost("127.0.0.1:0")
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Bind first so the guest knows the port.
	type res struct {
		c   *Conn
		err error
	}
	hostDone := make(chan res, 1)
	go func() { c, err := host.Establish(ctx); hostDone <- res{c, err} }()
	var addr string
	for addr == "" {
		host.mu.Lock()
		if host.ln != nil {
			addr = host.ln.Addr().String()
		}
		host.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}

	g, err := NewGuest(addr).Establish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h := <-hostDone
	if h.err != nil {
		t.Fatal(h.err)
	}
	if !h.c.P1() || g.P1() {
		t.Fatalf("roles: host P1=%v guest P1=%v", h.c.P1(), g.P1())
	}
	for i := range 200 {
		h.c.Send(byte(i))
		if b := <-g.Recv(); b != byte(i) {
			t.Fatalf("guest got %d, want %d", b, i)
		}
		g.Send(byte(255 - i))
		if b := <-h.c.Recv(); b != byte(255-i) {
			t.Fatalf("host got %d, want %d", b, 255-i)
		}
	}
	g.Close()
	select {
	case _, ok := <-h.c.Recv():
		if ok {
			t.Fatal("unexpected byte after close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host never saw the guest leave")
	}
}

// A connection that never echoes the ready byte (a port scanner, or a guest
// that gave up while queued in the backlog) is dropped, the real guest gets
// the game, and the port is closed again once it has.
func TestHostSkipsSilentConnection(t *testing.T) {
	host := NewHost("127.0.0.1:0")
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hostDone := make(chan *Conn, 1)
	go func() {
		c, err := host.Establish(ctx)
		if err != nil {
			t.Error(err)
		}
		hostDone <- c
	}()
	var addr string
	for addr == "" {
		host.mu.Lock()
		if host.ln != nil {
			addr = host.ln.Addr().String()
		}
		host.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}

	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()

	g, err := NewGuest(addr).Establish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	h := <-hostDone
	if h == nil {
		t.Fatal("host did not link up")
	}
	defer h.Close()

	h.Send(42)
	if b := <-g.Recv(); b != 42 {
		t.Fatalf("guest got %d, want 42", b)
	}
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Fatal("host still listening after the game linked up")
	}
}
