package webrtc

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc"
	"log"
	"time"
)

// webrtc command
// TextMessage
const (
	MessageTypeAnswer    = "answer"
	MessageTypeCandidate = "candidate"
	MessageTypeOffer     = "offer"
	MessageTypeInfo      = "info"

	// message start or ready needed?.
	// TODO: 准备阶段暂时不考虑start和ready的处理.（如果是webclient发起呼叫就需要处理这两类消息.）.
	MessageTypeStart     = "start"
	MessageTypeReady     = "ready"

	// connection control
	_maxMessageSize = 4096 // WebsocketMessage size.
	_writeWait       = 10 * time.Second
	_pongWait        = 2 * time.Minute // read time out.
	_pingPeriod      = time.Minute
)

type WebsocketMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// message signal hand:
// Signal is a wrapper of grpc
type Signal struct {
	// The websocket connection.
	conn *websocket.Conn
}

// NewSignal create a grpc signaler
func NewSignal(conn *websocket.Conn, webrtcConn *webrtc.PeerConnection) (*Signal, error) {
	s := &Signal{}
	s.id = id
	// Set up a connection to the sfu server.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure())
	if err != nil {
		log.Errorf("[%v] Connecting to sfu:%s failed: %v", s.id, addr, err)
		return nil, err
	}
	log.Infof("[%v] Connecting to sfu ok: %s", s.id, addr)

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.client = pb.NewSFUClient(conn)
	s.stream, err = s.client.Signal(s.ctx)
	if err != nil {
		log.Errorf("err=%v", err)
		return nil, err
	}
	return s, nil
}


// ReadLoop pumps messages from the websocket connection to the hub.
//
// The application runs ReadLoop in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Signal) SignalMessageLoop() {
	defer func() {
		// deregister
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(_maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(_pongWait))

	// nothing to do.
	c.conn.SetPongHandler(func(string) error { _ = c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	message := &WebsocketMessage{}
	for {
		// _, message, err := c.conn.ReadMessage()
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("could not read message: %s", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Println("ws closed unexpected")
			}
			return
		}

		err = json.Unmarshal(raw, &message)
		if err != nil {
			log.Printf("could not unmarshal ws message: %s", err)
			return
		}

		// if connect, just receive.
		// 流程参考: https://blog.csdn.net/fanhenghui/article/details/80229811.
		switch message.Event { //
		case MessageTypeCandidate: // receive ice server, ignored.(需要turn server的时候打开). 、、当offer端接收到来自对方的candidate时，pc.addIceCandidate(candidate);//将来自对方的candidate设置给本地.
			// only need candidate.
			candidate := webrtc.ICECandidateInit{}
			if err := json.Unmarshal(message.Data, &candidate); err != nil {
				errMessage := fmt.Sprintf("could not unmarshal candidate msg: %s", err)
				log.Printf("could not unmarshal candidate msg: %s", err)
				c.sendWarning(errMessage)
				return
			}

			if err := c.PeerConnection.AddICECandidate(candidate); err != nil {
				errMessage := fmt.Sprintf("Error taking candidate: %s", err)
				log.Printf("[webrtc=%v] %s", c.conn, errMessage)
				c.sendWarning(errMessage)
				return
			}

		case MessageTypeAnswer: // receive sdp answer.
			answer := webrtc.SessionDescription{}
			if err := json.Unmarshal(message.Data, &answer); err != nil {
				log.Printf("could not unmarshal answer msg: %s", err)
				return
			}

			if err := c.PeerConnection.SetRemoteDescription(answer); err != nil {
				log.Printf("could not set remote description: %s", err)
				return
			}

		default:
			errMessage := fmt.Sprintf("Received unknown command '%s'. Ignored.", message.Event)
			log.Printf("[webrtc=%v] %s", c.conn, errMessage)
			c.sendWarning(errMessage)
		}
	}
}

func (c *Client) Close()  {
	_ = c.conn.Close()
	close(c.Send)
}

// WriteLoop pumps messages from the hub to the websocket connection.
// A goroutine running WriteLoop is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) WriteWebrtcMessageLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.Send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// why not use WriteMessage?, Writer只有一个.
			// TODO: 和WriteMessage比较, flush 了之前写入的内容..
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, err = w.Write(message)
			if err != nil {
				log.Printf("could not send message: %s",err)
				_ = w.Close()
				return
			}

			if err := w.Close(); err != nil {
				return
			}

		// 	Heart break Message
		case <-ticker.C: // Server to client heart-break.
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				// server break.
				return
			}
		}
	}
}


func (c *Client) SendObject(messageStruct interface{}) error {
	message, err := json.Marshal(messageStruct)
	c.Send <- message
	return err
	// _ = c.conn.WriteMessage(websocket.TextMessage, message)
}

func (c *Client) SendMessage(message []byte) {
	c.Send <- message
	// _ = c.conn.WriteMessage(websocket.TextMessage, message)
}

func (c *Client) sendWarning(text string) error {
	return c.SendObject(struct {
		Warning string `json:"warning"`
	}{text})
}

func (c *Client) sendError(text string) error {
	return c.SendObject(struct {
		Error string `json:"error"`
	}{text})
}

func (c *Client) sendStatus(text string) error {
	return c.SendObject(struct {
		Status string `json:"status"`
	}{text})
}