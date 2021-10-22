package usecase

import (
	"github.com/gorilla/websocket"
	"sync"
)

// Helper to make Gorilla Websockets threadsafe
type ThreadSafeWriter struct {
	*websocket.Conn
	sync.Mutex
}