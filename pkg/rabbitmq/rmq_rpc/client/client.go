package client

import (
	"common/broker"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	rmqrpc "github.com/evrone/go-clean-template/pkg/rabbitmq/rmq_rpc"
	"github.com/google/uuid"
)

// ErrConnectionClosed -.
var ErrConnectionClosed = errors.New("rmq_rpc client - Client - RemoteCall - Connection closed")

const (
	_defaultWaitTime = 5 * time.Second
	_defaultAttempts = 10
	_defaultTimeout  = 2 * time.Second
)

// Message -.
type Message struct {
	Queue         string
	Priority      uint8
	ContentType   string
	Body          []byte
	ReplyTo       string
	CorrelationID string
}

type pendingCall struct {
	done   chan struct{}
	status string // handler
	body   []byte
}

// Client -.
type Client struct {
	conn           *rmqrpc.Connection
	serverExchange string
	error          chan error
	stop           chan struct{}

	sync.RWMutex
	calls map[string]*pendingCall

	timeout time.Duration
}

// New -.
func New(url, serverExchange, clientExchange, topic string, opts ...Option) (*Client, error) {
	cfg := rmqrpc.Config{
		URL:      url,
		WaitTime: _defaultWaitTime,
		Attempts: _defaultAttempts,
	}

	c := &Client{
		conn:           rmqrpc.New(clientExchange, topic, cfg),
		serverExchange: serverExchange,
		error:          make(chan error),
		stop:           make(chan struct{}),
		calls:          make(map[string]*pendingCall),
		timeout:        _defaultTimeout,
	}

	// Custom options
	for _, opt := range opts {
		opt(c)
	}

	err := c.conn.AttemptConnect()
	if err != nil {
		return nil, fmt.Errorf("rmq_rpc client - NewClient - c.conn.AttemptConnect: %w", err)
	}

	go c.consumer()

	return c, nil
}

func (c *Client) publish(corrID, handler string, request interface{}) error {
	var (
		requestBody []byte
		err         error
	)

	if request != nil {
		requestBody, err = json.Marshal(request)
		if err != nil {
			return err
		}
	}

	// use generated id for message
	msg := &broker.Message{
		Header: map[string]string{
			rmqrpc.MessageCorId: corrID,
			rmqrpc.MessageType: handler,
		},
		Body: requestBody,
	}

	// key = topic.
	//err = c.conn.Channel.Publish(c.serverExchange, "", false, false,
	//	amqp.Publishing{
	//		ContentType:   "application/json",
	//		CorrelationId: corrID,
	//		ReplyTo:       c.conn.ConsumerExchange,
	//		Type:          handler,
	//		Body:          requestBody,
	//	})
	err = c.conn.RbBroker.Publish(c.conn.Topic, msg)
	if err != nil {
		return fmt.Errorf("c.Channel.Publish: %w", err)
	}

	return nil
}

// RemoteCall -.
func (c *Client) RemoteCall(handler string, request, response interface{}) error { //nolint:cyclop // complex func
	select {
	case <-c.stop:
		time.Sleep(c.timeout)
		select {
		case <-c.stop:
			return ErrConnectionClosed
		default:
		}
		default:
	}

	corrID := uuid.New().String()

	err := c.publish(corrID, handler, request)
	if err != nil {
		return fmt.Errorf("rmq_rpc client - Client - RemoteCall - c.publish: %w", err)
	}

	call := &pendingCall{done: make(chan struct{})}

	c.addCall(corrID, call)
	defer c.deleteCall(corrID)

	select {
	case <-time.After(c.timeout):
		return rmqrpc.ErrTimeout
	case <-call.done:
	}

	if call.status == rmqrpc.Success {
		err = json.Unmarshal(call.body, &response)
		if err != nil {
			return fmt.Errorf("rmq_rpc client - Client - RemoteCall - json.Unmarshal: %w", err)
		}

		return nil
	}

	if call.status == rmqrpc.ErrBadHandler.Error() {
		return rmqrpc.ErrBadHandler
	}

	if call.status == rmqrpc.ErrInternalServer.Error() {
		return rmqrpc.ErrInternalServer
	}

	return nil
}

func (c *Client) consumer() {
	for {
		select {
		case <-c.stop:
			return
		case d, opened := <-c.conn.Delivery:
			if !opened {
				c.reconnect()

				return
			}
			// _ = d.Ack(false) //nolint:errcheck // don't need this
			c.getCall(&d)
		}
	}
}

func (c *Client) reconnect() {
	close(c.stop)

	err := c.conn.AttemptConnect()
	if err != nil {
		c.error <- err
		close(c.error)

		return
	}

	c.stop = make(chan struct{})

	go c.consumer()
}

func (c *Client) getCall(d *broker.Message) {
	c.RLock()
	call, ok := c.calls[d.Header[rmqrpc.MessageCorId]]
	c.RUnlock()

	if !ok {
		return
	}

	call.status = d.Header[rmqrpc.MessageType] // d.Type.
	call.body = d.Body
	close(call.done)
}

func (c *Client) addCall(corrID string, call *pendingCall) {
	c.Lock()
	c.calls[corrID] = call
	c.Unlock()
}

func (c *Client) deleteCall(corrID string) {
	c.Lock()
	delete(c.calls, corrID)
	c.Unlock()
}

// Notify -.
func (c *Client) Notify() <-chan error {
	return c.error
}

// Shutdown -.
func (c *Client) Shutdown() error {
	select {
	case <-c.error:
		return nil
	default:
	}

	close(c.stop)
	time.Sleep(c.timeout)

	err := c.conn.RbBroker.Disconnect() // shutdown
	if err != nil {
		return fmt.Errorf("rmq_rpc client - Client - Shutdown - c.Connection.Close: %w", err)
	}

	return nil
}
