// Package redis implements a minimal RESP2 Redis client.
// Supports exactly the commands needed: PING, ZADD, ZRANGEBYSCORE, ZREVRANGE, and pipelining.
package redis

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Client is a minimal Redis client with connection pooling.
type Client struct {
	pool *Pool
}

// NewClient creates a client connected to addr (e.g. "localhost:6379").
func NewClient(addr string) (*Client, error) {
	pool := NewPool(addr, 16, 30*time.Second)
	return &Client{pool: pool}, nil
}

// Close releases all connections.
func (c *Client) Close() error {
	c.pool.Close()
	return nil
}

// Ping checks connectivity. Returns nil on PONG.
func (c *Client) Ping(ctx context.Context) error {
	conn, err := c.pool.Get(ctx)
	if err != nil {
		return err
	}
	defer c.pool.Put(conn)

	return conn.Do(ctx, "PING").ExpectSimple("PONG")
}

// ZADD adds a member with a score to a sorted set.
func (c *Client) ZADD(ctx context.Context, key string, score int64, member []byte) error {
	conn, err := c.pool.Get(ctx)
	if err != nil {
		return err
	}
	defer c.pool.Put(conn)

	return conn.Do(ctx, "ZADD", key, score, member).ExpectInt()
}

// ZRANGEBYSCORE returns members in [min, max] with optional LIMIT offset count.
func (c *Client) ZRANGEBYSCORE(ctx context.Context, key string, min, max int64, offset, count int) ([][]byte, error) {
	conn, err := c.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer c.pool.Put(conn)

	if offset > 0 || count > 0 {
		return conn.Do(ctx, "ZRANGEBYSCORE", key, min, max,
			"LIMIT", offset, count).ExpectBulkArray()
	}
	return conn.Do(ctx, "ZRANGEBYSCORE", key, min, max).ExpectBulkArray()
}

// ZREVRANGE returns members from start to stop in reverse order (highest score first).
func (c *Client) ZREVRANGE(ctx context.Context, key string, start, stop int) ([][]byte, error) {
	conn, err := c.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer c.pool.Put(conn)

	return conn.Do(ctx, "ZREVRANGE", key, start, stop).ExpectBulkArray()
}

// Pipeline collects commands and flushes them in a single round trip.
type Pipeline struct {
	client *Client
	conn   *Conn
	cmds   []*Result
}

// Pipeline creates a new pipeline. Call Pipeline() to get a builder, then Exec() to flush.
func (c *Client) Pipeline(ctx context.Context) (*Pipeline, error) {
	conn, err := c.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &Pipeline{client: c, conn: conn}, nil
}

// ZADD queues a ZADD command.
func (p *Pipeline) ZADD(key string, score int64, member []byte) *Result {
	return p.queue("ZADD", key, score, member)
}

// Exec flushes all queued commands and reads all responses. Returns the connection to the pool.
func (p *Pipeline) Exec(ctx context.Context) error {
	defer p.client.pool.Put(p.conn)

	if len(p.cmds) == 0 {
		return nil
	}

	// Write all commands.
	for _, r := range p.cmds {
		if err := p.conn.writeCommand(r.cmd...); err != nil {
			return fmt.Errorf("pipeline write: %w", err)
		}
	}
	if err := p.conn.wr.Flush(); err != nil {
		return fmt.Errorf("pipeline flush: %w", err)
	}

	// Read all responses in order.
	for _, r := range p.cmds {
		resp, err := p.conn.readResponse()
		if err != nil {
			r.err = err
		} else {
			r.resp = resp
		}
	}
	return nil
}

func (p *Pipeline) queue(args ...any) *Result {
	r := &Result{cmd: args}
	p.cmds = append(p.cmds, r)
	return r
}

// Result holds the outcome of a pipelined command.
type Result struct {
	cmd  []any
	resp any
	err  error
}

// ExpectSimple checks the response is a simple string matching want.
func (r *Result) ExpectSimple(want string) error {
	if r.err != nil {
		return r.err
	}
	s, ok := r.resp.(string)
	if !ok {
		return fmt.Errorf("expected simple string, got %T", r.resp)
	}
	if s != want {
		return fmt.Errorf("expected %q, got %q", want, s)
	}
	return nil
}

// ExpectInt checks the response is an integer (no error).
func (r *Result) ExpectInt() error {
	if r.err != nil {
		return r.err
	}
	return expectInt(r.resp)
}

// ExpectBulkArray checks the response is a bulk array and returns the members.
func (r *Result) ExpectBulkArray() ([][]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return expectBulkArray(r.resp)
}

// ── connection ──────────────────────────────────────────

// Conn wraps a single TCP connection with RESP2 read/write.
type Conn struct {
	nc  net.Conn
	wr  *bufio.Writer
	rd  *bufio.Reader
}

func newConn(addr string, timeout time.Duration) (*Conn, error) {
	nc, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Conn{
		nc: nc,
		wr: bufio.NewWriterSize(nc, 4096),
		rd: bufio.NewReaderSize(nc, 4096),
	}, nil
}

func (c *Conn) Close() error {
	return c.nc.Close()
}

// Do executes a single command and returns the parsed response.
func (c *Conn) Do(ctx context.Context, args ...any) *Result {
	if err := c.writeCommand(args...); err != nil {
		return &Result{err: err}
	}
	if err := c.wr.Flush(); err != nil {
		return &Result{err: err}
	}
	resp, err := c.readResponse()
	return &Result{resp: resp, err: err}
}

// writeCommand writes a RESP2 command: *<N>\r\n$<len>\r\n<arg>\r\n...
// Uses a pooled scratch buffer to avoid allocations from fmt.Fprintf.
func (c *Conn) writeCommand(args ...any) error {
	var scratch [32]byte

	// Write array header.
	c.wr.WriteByte('*')
	c.wr.Write(strconv.AppendInt(scratch[:0], int64(len(args)), 10))
	c.wr.WriteString("\r\n")

	for _, arg := range args {
		c.wr.WriteByte('$')
		switch v := arg.(type) {
		case string:
			c.wr.Write(strconv.AppendInt(scratch[:0], int64(len(v)), 10))
			c.wr.WriteString("\r\n")
			c.wr.WriteString(v)
			c.wr.WriteString("\r\n")
		case []byte:
			c.wr.Write(strconv.AppendInt(scratch[:0], int64(len(v)), 10))
			c.wr.WriteString("\r\n")
			c.wr.Write(v)
			c.wr.WriteString("\r\n")
		case int64:
			s := strconv.FormatInt(v, 10)
			c.wr.Write(strconv.AppendInt(scratch[:0], int64(len(s)), 10))
			c.wr.WriteString("\r\n")
			c.wr.WriteString(s)
			c.wr.WriteString("\r\n")
		case int:
			s := strconv.Itoa(v)
			c.wr.Write(strconv.AppendInt(scratch[:0], int64(len(s)), 10))
			c.wr.WriteString("\r\n")
			c.wr.WriteString(s)
			c.wr.WriteString("\r\n")
		default:
			// Fallback for any other type (e.g. itoa returns string for int scores).
			s := fmt.Sprint(v)
			c.wr.Write(strconv.AppendInt(scratch[:0], int64(len(s)), 10))
			c.wr.WriteString("\r\n")
			c.wr.WriteString(s)
			c.wr.WriteString("\r\n")
		}
	}
	return nil
}

// readResponse reads a single RESP2 response.
func (c *Conn) readResponse() (any, error) {
	typ, err := c.rd.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read type: %w", err)
	}

	switch typ {
	case '+': // simple string
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		return string(line), nil

	case '-': // error
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("redis: %s", string(line))

	case ':': // integer
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse int: %w", err)
		}
		return n, nil

	case '$': // bulk string
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(string(line))
		if err != nil {
			return nil, fmt.Errorf("parse bulk len: %w", err)
		}
		if length < 0 {
			return nil, nil // null bulk string
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(c.rd, buf); err != nil {
			return nil, fmt.Errorf("read bulk: %w", err)
		}
		return buf[:length], nil

	case '*': // array
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		count, err := strconv.Atoi(string(line))
		if err != nil {
			return nil, fmt.Errorf("parse array len: %w", err)
		}
		if count < 0 {
			return nil, nil
		}
		arr := make([][]byte, count)
		for i := range count {
			elem, err := c.readBulkString()
			if err != nil {
				return nil, fmt.Errorf("array[%d]: %w", i, err)
			}
			arr[i] = elem
		}
		return arr, nil

	default:
		return nil, fmt.Errorf("unknown response type: %c", typ)
	}
}

func (c *Conn) readBulkString() ([]byte, error) {
	typ, err := c.rd.ReadByte()
	if err != nil {
		return nil, err
	}
	if typ != '$' {
		return nil, fmt.Errorf("expected '$', got '%c'", typ)
	}
	line, err := c.readLine()
	if err != nil {
		return nil, err
	}
	length, err := strconv.Atoi(string(line))
	if err != nil {
		return nil, err
	}
	if length < 0 {
		return nil, nil
	}
	buf := make([]byte, length+2)
	if _, err := io.ReadFull(c.rd, buf); err != nil {
		return nil, err
	}
	return buf[:length], nil
}

func (c *Conn) readLine() ([]byte, error) {
	line, err := c.rd.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("invalid line ending")
	}
	return line[:len(line)-2], nil
}

func expectInt(resp any) error {
	if err, ok := resp.(error); ok {
		return err
	}
	if _, ok := resp.(int64); !ok {
		return fmt.Errorf("expected int64, got %T", resp)
	}
	return nil
}

func expectBulkArray(resp any) ([][]byte, error) {
	if err, ok := resp.(error); ok {
		return nil, err
	}
	arr, ok := resp.([][]byte)
	if !ok {
		return nil, fmt.Errorf("expected [][]byte, got %T", resp)
	}
	return arr, nil
}

// ── helpers ─────────────────────────────────────────────
