package redis

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Pool manages a bounded set of reusable Redis connections.
type Pool struct {
	addr    string
	timeout time.Duration
	ch      chan *Conn
	mu      sync.Mutex
	closed  bool
}

// NewPool creates a connection pool of size connections to addr.
// All connections are pre-connected eagerly.
func NewPool(addr string, size int, timeout time.Duration) *Pool {
	p := &Pool{
		addr:    addr,
		timeout: timeout,
		ch:      make(chan *Conn, size),
	}
	for range size {
		conn, err := newConn(addr, timeout)
		if err != nil {
			continue
		}
		p.ch <- conn
	}
	return p
}

// Get returns a connection from the pool, blocking if none available.
func (p *Pool) Get(ctx context.Context) (*Conn, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("pool closed")
	}
	p.mu.Unlock()

	select {
	case conn := <-p.ch:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Put returns a connection to the pool.
func (p *Pool) Put(conn *Conn) {
	if conn == nil {
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		conn.Close()
		return
	}
	select {
	case p.ch <- conn:
	default:
		conn.Close()
	}
}

// Close drains and closes all connections.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	close(p.ch)
	for conn := range p.ch {
		conn.Close()
	}
}

// Len returns the number of idle connections in the pool.
func (p *Pool) Len() int { return len(p.ch) }
