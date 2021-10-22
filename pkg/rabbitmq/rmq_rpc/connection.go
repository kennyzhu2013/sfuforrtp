package rmqrpc

import (
	"common/broker"
	"common/rabbitmq"
	"fmt"
	"log"
	"time"
)

const (
	MessageCorId   = "CorrelationId"
	MessageType    = "Type"
)

// Config -.
type Config struct {
	URL      string
	// Topic    string
	WaitTime time.Duration
	Attempts int
}

// Connection -.
type Connection struct {
	ConsumerExchange string
	Topic    string
	Config
	RbBroker broker.Broker
	Delivery <-chan broker.Message
}

// New -.
func New(consumerExchange, topic string, cfg Config) *Connection {
	conn := &Connection{
		ConsumerExchange: consumerExchange,
		Topic:            topic,
		Config:           cfg,
		RbBroker:         rabbitmq.NewBroker(),
	}

	return conn
}

// AttemptConnect -.
func (c *Connection) AttemptConnect() error {
	var err error
	for i := c.Attempts; i > 0; i-- {
		if err = c.connect(); err == nil {
			break
		}

		log.Printf("RabbitMQ is trying to connect, attempts left: %d", i)
		time.Sleep(c.WaitTime)
	}

	if err != nil {
		return fmt.Errorf("rmq_rpc - AttemptConnect - c.connect: %w", err)
	}

	return nil
}

func (c *Connection) connect() error {

	_ = c.RbBroker.Init(broker.Addrs(c.URL), rabbitmq.Exchange(c.ConsumerExchange), rabbitmq.KindExchange(c.Topic)) //topic.
	if err := c.RbBroker.Connect(); err != nil {
		return fmt.Errorf("amqp.Dial: %w", err)
	}

	//c.Channel, err = c.Connection.Channel()
	//if err != nil {
	//	return fmt.Errorf("c.Connection.Channel: %w", err)
	//}
	//
	//// 广播? why?...
	//err = c.Channel.ExchangeDeclare(
	//	c.ConsumerExchange,
	//	"fanout",
	//	false,
	//	false,
	//	false,
	//	false,
	//	nil,
	//)
	//if err != nil {
	//	return fmt.Errorf("c.Connection.Channel: %w", err)
	//}
	//
	//queue, err := c.Channel.QueueDeclare(
	//	"",
	//	false,
	//	false,
	//	true,
	//	false,
	//	nil,
	//)
	//if err != nil {
	//	return fmt.Errorf("c.Channel.QueueDeclare: %w", err)
	//}
	//
	//err = c.Channel.QueueBind(
	//	queue.Name,
	//	"",
	//	c.ConsumerExchange,
	//	false,
	//	nil,
	//)
	//if err != nil {
	//	return fmt.Errorf("c.Channel.QueueBind: %w", err)
	//}
	//
	//c.Delivery, err = c.Channel.Consume(
	//	queue.Name,
	//	"",
	//	false,
	//	false,
	//	false,
	//	false,
	//	nil,
	//)
	//if err != nil {
	//	return fmt.Errorf("c.Channel.Consume: %w", err)
	//}
	return nil
}

// pub message directly for test.
func (c *Connection) pubErrorTest(id string, body []byte) error {
	// use generated id for message.
	msg := &broker.Message{
		Header: map[string]string{
			MessageType: id,
		},
		Body: body,
	}

	err := c.RbBroker.Publish(c.Topic, msg)
	if err != nil {
		return fmt.Errorf("RbBroker.Publish: %w", err)
	}
	return nil
}
