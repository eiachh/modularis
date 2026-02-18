package hub

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/modularis/modularis/internal/domain"
)

// Conn wraps a raw gorilla WebSocket connection with thread-safe write
// access. The caller attaches domain metadata after registration via the
// exported Metadata field.
type Conn struct {
	Raw *websocket.Conn

	// Metadata holds the domain object associated with this connection.
	// For agent connections this will be *domain.Agent, for display
	// connections *domain.Display. Nil until registration completes.
	Metadata any

	writeMu sync.Mutex
}

// WriteJSON serialises v as JSON and sends it on the WebSocket. It is safe
// to call from multiple goroutines.
func (c *Conn) WriteJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.Raw.WriteJSON(v)
}

// ReadJSON reads a single JSON message from the WebSocket into v.
func (c *Conn) ReadJSON(v any) error {
	return c.Raw.ReadJSON(v)
}

// SendEnvelope is a convenience method that builds an Envelope, marshals the
// payload, and writes it in one step.
func (c *Conn) SendEnvelope(msgType domain.MessageType, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.WriteJSON(domain.Envelope{
		Type:    msgType,
		Payload: raw,
	})
}

// Close gracefully closes the underlying WebSocket connection.
func (c *Conn) Close() error {
	return c.Raw.Close()
}
