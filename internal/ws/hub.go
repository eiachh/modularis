package ws

import (
	"log/slog"
	"sync"

	"github.com/modularis/modularis/internal/domain"
)

// Hub keeps track of every active WebSocket connection (keyed by ID after
// registration). It supports targeted sends and fan-out broadcasts, and
// ensures clean-up when connections drop.
type Hub struct {
	mu    sync.RWMutex
	conns map[string]*Conn // ID → *Conn
	log   *slog.Logger
	label string // human-readable label for log messages (e.g. "agent", "display")
}

// Hubs groups the agent and display hubs for shared wiring.
type Hubs struct {
	Agent   *Hub
	Display *Hub
}

// NewHub creates a Hub ready to accept connections.
// The label is used in log messages to distinguish between agent and display hubs.
func NewHub(log *slog.Logger, label string) *Hub {
	return &Hub{
		conns: make(map[string]*Conn),
		log:   log,
		label: label,
	}
}

// Register adds a fully-registered connection to the hub.
func (h *Hub) Register(id string, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[id] = c
	h.log.Info(h.label+" registered in hub", "id", id)
}

// Unregister removes a connection from the hub and closes it.
func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.conns[id]; ok {
		_ = c.Close()
		delete(h.conns, id)
		h.log.Info(h.label+" unregistered from hub", "id", id)
	}
}

// Get returns the connection for a given ID, or nil.
func (h *Hub) Get(id string) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[id]
}

// Count returns the number of active connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// Broadcast sends an envelope to every connection in the hub. Failures on
// individual connections are logged but do not stop the broadcast.
func (h *Hub) Broadcast(msgType domain.MessageType, payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for id, c := range h.conns {
		if err := c.SendEnvelope(msgType, payload); err != nil {
			h.log.Warn("broadcast send failed", "id", id, "error", err)
		}
	}
}
