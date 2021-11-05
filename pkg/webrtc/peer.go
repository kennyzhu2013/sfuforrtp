package webrtc

import (
	"github.com/pion/webrtc/v3"
	"sync"
)

// local peer: only one tack.
type Peer interface {
	ID() string
	Session() Session
	Publisher() *Publisher
	Subscriber() *Subscriber
	Close() error

	// 这个版本不支持channel.
	// SendDCMessage(label string, msg []byte) error
}

// SessionProvider provides the SessionLocal to the sfu.Peer
// This allows the sfu.SFU{} implementation to be customized / wrapped by another package
type SessionProvider interface {
	GetSession(sid string) (Session, WebRTCTransportConfig)
}

// PeerLocal represents a pair peer connection
// include state.
type PeerLocal struct {
	sync.Mutex
	id       string
	closed   atomicBool
	session  Session
	provider SessionProvider

	publisher  *Publisher
	subscriber *Subscriber

	// change to events.
	OnOffer                    func(*webrtc.SessionDescription)
	OnIceCandidate             func(*webrtc.ICECandidateInit, int)
	OnICEConnectionStateChange func(webrtc.ICEConnectionState)

	remoteAnswerPending bool
	negotiationPending  bool
}