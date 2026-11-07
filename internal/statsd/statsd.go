// Package statsd is a minimal fire-and-forget StatsD client over UDP.
package statsd

import (
	"fmt"
	"net"
	"time"
)

// Client sends metrics to a StatsD daemon. A nil *Client is a valid no-op.
type Client struct {
	conn net.Conn
}

// New dials addr (host:port) over UDP/IPv4 (what StatsD daemons listen on by default). UDP "connections" don't handshake, so
// this only fails on a bad address; sends to an absent daemon are ignored.
func New(addr string) (*Client, error) {
	conn, err := net.Dial("udp4", addr)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func (c *Client) send(format string, args ...any) {
	if c == nil || c.conn == nil {
		return
	}
	_, _ = c.conn.Write([]byte(fmt.Sprintf(format, args...)))
}

// Increment bumps a counter by n.
func (c *Client) Increment(name string, n int) { c.send("%s:%d|c", name, n) }

// Timing records a duration in milliseconds.
func (c *Client) Timing(name string, d time.Duration) { c.send("%s:%d|ms", name, d.Milliseconds()) }

// Close releases the socket.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
