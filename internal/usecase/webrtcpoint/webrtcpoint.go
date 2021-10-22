package webrtcpoint

import (
	"github.com/pion/webrtc/v3"
	"mediasfu/internal/usecase"
	"sync"
)

type peerConnectionState struct {
	peerConnection *webrtc.PeerConnection
	websocket      *usecase.ThreadSafeWriter
}

// Point include sources or sinks.
type Point struct {
	// lock for peerConnections and trackLocals
	listLock        sync.RWMutex
	peerConnections []peerConnectionState
	trackLocals  map[string]*webrtc.TrackLocalStaticRTP
}
