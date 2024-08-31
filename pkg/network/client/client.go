package client

import (
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/EncrypteDL/CryptFS/pkg/message"
	log "github.com/sirupsen/logrus"
)

var (
	//ErrTimeout is the error returned when  things time out
	ErrTimeout = errors.New("timeout")
)

type options struct {
	address            string
	fallBackToPlainTCP bool
}

// Option is a client functional option for configuring the client
type Option func(*options)

// WithAddress sets the address for the client co connect to
func WithAddress(value string) Option {
	return func(o *options) {
		o.address = value
	}
}

// WithFallbackToPlainTCP configures the client to fallback to plain unsecured TCP
func WithFallbackToPlainTCP() Option {
	return func(o *options) {
		o.fallBackToPlainTCP = true
	}
}

// Client is a low-level metadata server client that can send and receive
// message.Message's. It can be used to build higher level clients, e.g., a
// storage.VersionedStore implementation.
type Client struct {
	opts options

	// Both will use a net.Conn to write to and read from. It will usually be
	// the conn property below, but might not be around the time of
	// reconnection.
	encoder *message.Encoder
	decoder *message.Decoder

	mu   sync.Mutex
	conn net.Conn
}

// New creates an instances of the client with the provided options
func New(opts ...Option) *Client {
	var c Client
	c.opts.address = "tcp://127.0.0.1:8000"
	c.encoder = new(message.Encoder)
	c.decoder = new(message.Decoder)
	for _, o := range opts {
		o(&c.opts)
	}
	return &c
}

// Close closes the client connection
func (c *Client) Close() {
	c.closeBoth(nil)
}

func (c *Client) closeBoth(cached net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached != nil && cached != c.conn {
		logger := log.WithFields(log.Fields{
			"local":  cached.LocalAddr(),
			"remote": cached.RemoteAddr(),
		})
		logger.Debug("Closing cached connection")
		if err := cached.Close(); err != nil {
			logger.WithField("err", err).Warn("Could not close cached connection")
		}
	}
	if c.conn != nil {
		logger := log.WithFields(log.Fields{
			"local":  c.conn.LocalAddr(),
			"remote": c.conn.RemoteAddr(),
		})
		logger.Debug("Closing own connection")
		if err := c.conn.Close(); err != nil {
			logger.WithField("err", err).Warn("Could not close current connection")
		}
		c.conn = nil
	}
}

// Send sends the message to the server.
func (c *Client) Send(m message.Message) error {
	return c.doWithConn(func(conn net.Conn) error {
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		return c.encoder.Encode(conn, m)
	})
}

// Receive receives a message from the server.
func (c *Client) Receive(m *message.Message) error {
	return c.doWithConn(func(conn net.Conn) error {
		return c.decoder.Decode(conn, m)
	})
}

func (c *Client) doWithConn(consumer func(net.Conn) error) error {
	conn, err := c.getCachedConn()
	if err != nil {
		c.closeBoth(conn)
		return err
	}
	if err := consumer(conn); err != nil {
		c.closeBoth(conn)
		return err
	}
	return nil
}

func (c *Client) getCachedConn() (conn net.Conn, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn, nil
	}
	if strings.HasPrefix(c.opts.address, "tls://") {
		conn, err = tls.Dial("tcp", strings.TrimPrefix(c.opts.address, "tls://"), nil)
		if err != nil && c.opts.fallBackToPlainTCP {
			log.WithField("err", err).Warn("Could not dial using TLS, trying plain TCP")
			conn, err = net.Dial("tcp", c.opts.address)
		}
	} else {
		conn, err = net.Dial("tcp", c.opts.address)
	}
	if err != nil {
		return nil, err
	}
	c.conn = conn
	return conn, nil
}
