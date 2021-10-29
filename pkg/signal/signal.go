package signal

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"log"
	"sync"
	"time"
)

// webrtc and rtp command
// TextMessage
const (
	// RTP和WebRtc保持一致.
	// 简化结构.
	MessageTypeAnswer    = "answer"
	MessageTypeCandidate = "candidate"
	MessageTypeOffer     = "offer"

	// join 会议系统，参考ion sfu.
	MessageTypeJoin      = "join"

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

// TODO: add Join message..
type JoinMessage struct {
	Id   string			`json:"id"`  // for bill_id.
	Description  []byte `json:"sdp"`
}

// for all messages.
type WebsocketMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// message signal hand:
// client /server ---> signal layer ---> Session
// process all signal process for ws message.
type Signal struct {
	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	Send chan []byte

	// Need to rewrite.
	OnNegotiate    func(*webrtc.SessionDescription) error
	OnSetRemoteSDP func(*webrtc.SessionDescription) error
	OnError        func(error)
	OnTrickle      func(*webrtc.ICECandidateInit)



	// use Session  instead.
	PeerConnection *webrtc.PeerConnection
	once sync.Once
}



// NewSignal create a ws signaler
func NewSignal(conn *websocket.Conn, webrtcConn *webrtc.PeerConnection) *Signal{
	return &Signal{
		conn:           conn,
		Send:           make(chan []byte),
		PeerConnection: webrtcConn,
	}
}

// add init func.

// ReadLoop pumps messages from the websocket connection to the hub.
// The application runs ReadLoop in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
// process all signal include server and clients.
func (s *Signal) SignalMessageLoop() {
	defer func() {
		// deregister
		_ = s.conn.Close()
	}()
	s.conn.SetReadLimit(_maxMessageSize)
	_ = s.conn.SetReadDeadline(time.Now().Add(_pongWait))

	// nothing to do.
	s.conn.SetPongHandler(func(string) error { _ = s.conn.SetReadDeadline(time.Now().Add(_pongWait)); return nil })
	message := &WebsocketMessage{}
	for {
		// _, message, err := s.conn.ReadMessage()
		// wait until message.
		messageType, raw, err := s.conn.ReadMessage()
		if err != nil {
			log.Printf("could not read message: %s", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Println("ws closed unexpected")
			}
			return
		} else if messageType == websocket.CloseMessage {
			log.Printf("Peer close conn, message: CloseMessage")
			s.Close()
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
				_ = s.sendWarning(errMessage)
				return
			}

			if s.OnTrickle != nil {
				s.OnTrickle(&candidate)
				return
			} else {
				if err := s.PeerConnection.AddICECandidate(candidate); err != nil {
					errMessage := fmt.Sprintf("Error taking candidate: %s", err)
					log.Printf("[webrtc=%v] %s", s.conn, errMessage)
					_ = s.sendWarning(errMessage)
					return
				}
			}

		case MessageTypeAnswer: // receive sdp answer.
			answer := webrtc.SessionDescription{}
			if err := json.Unmarshal(message.Data, &answer); err != nil {
				log.Printf("could not unmarshal answer msg: %s", err)
				return
			}

			if s.OnSetRemoteSDP != nil {
				_ = s.OnSetRemoteSDP(&answer)
				return
			} else {
				if err := s.PeerConnection.SetRemoteDescription(answer); err != nil {
					log.Printf("could not set remote description: %s", err)
					return
				}
			}

		case MessageTypeOffer: // as server.
			offer := webrtc.SessionDescription{}
			if err := json.Unmarshal(message.Data, &offer); err != nil {
				log.Printf("could not unmarshal answer msg: %s", err)
				return
			}

			if s.OnNegotiate != nil {
				_ = s.OnNegotiate(&offer)
				return
			} else { // default handle.
				err := s.takeOffer(&offer)
				if nil != err {
					log.Printf("could not takeOffer: %s", err)
					return
				}

				// send answer.
				answer := s.PeerConnection.LocalDescription()
				_ = s.SendObject(*answer)
			}

		// TODO: 增加join命令支持会议.
		case MessageTypeJoin:

			fallthrough

		default:
			errMessage := fmt.Sprintf("Received unknown command '%s'. Ignored.", message.Event)
			log.Printf("[webrtc=%v] %s", s.conn, errMessage)
			_ = s.sendWarning(errMessage)
		}
	}
}

func (s *Signal) takeOffer(offer *webrtc.SessionDescription) error {
	if err := s.PeerConnection.SetRemoteDescription(*offer); nil != err {
		log.Printf("could not set remote description: %s", err)
		return err
	}

	answer, err := s.PeerConnection.CreateAnswer(nil)
	if nil != err {
		log.Printf("could not create answer: %s", err)
		return err
	}

	return s.PeerConnection.SetLocalDescription(answer)
}


// WriteLoop pumps messages from the hub to the websocket connection.
// A goroutine running WriteLoop is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (s *Signal) WriteWebrtcMessageLoop() {
	ticker := time.NewTicker(_pingPeriod)
	defer func() {
		ticker.Stop()
		_ = s.conn.Close()
	}()
	for {
		select {
		case message, ok := <-s.Send:
			_ = s.conn.SetWriteDeadline(time.Now().Add(_writeWait))
			if !ok {
				// Closed the channel.
				_ = s.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// why not use WriteMessage?, Writer只有一个.
			// TODO: 和WriteMessage比较, flush 了之前写入的内容..
			w, err := s.conn.NextWriter(websocket.TextMessage)
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
		// ping pong message.
		case <-ticker.C: // Server to client heart-break.
			_ = s.conn.SetWriteDeadline(time.Now().Add(_writeWait))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				// server break.
				return
			}
		}
	}
}

// On Call back.
// for Peer Local Stage change. put into peerLocal.
// ALL Send message.
// TODO: Send message.

func (s *Signal) Close()  {
	s.once.Do( func() {close(s.Send)} )
}

func (s *Signal) SendObject(messageStruct interface{}) error {
	message, err := json.Marshal(messageStruct)
	s.Send <- message
	return err
	// _ = s.conn.WriteMessage(websocket.TextMessage, message)
}

func (s *Signal) SendMessage(message []byte) {
	s.Send <- message
	// _ = s.conn.WriteMessage(websocket.TextMessage, message)
}

func (s *Signal) sendWarning(text string) error {
	return s.SendObject(struct {
		Warning string `json:"warning"`
	}{text})
}

func (s *Signal) sendError(text string) error {
	return s.SendObject(struct {
		Error string `json:"error"`
	}{text})
}

func (s *Signal) sendStatus(text string) error {
	return s.SendObject(struct {
		Status string `json:"status"`
	}{text})
}
