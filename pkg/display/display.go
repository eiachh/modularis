// Package display provides a client library for connecting a display module
// to the Modularis orchestrator. It handles the WebSocket connection,
// registration handshake, and message delivery.
package display

import (
	"encoding/json"
	"fmt"

	"github.com/gorilla/websocket"

	"github.com/modularis/modularis/internal/domain"
)

const connectPath = "/display"

// Display represents an active display connection to the orchestrator.
type Display struct {
	ID   string
	Name string
	conn *websocket.Conn
}

// Connect dials the orchestrator at the given base URL and returns a Display
// ready to be registered. baseURL should be e.g. "ws://localhost:8080".
func Connect(baseURL string) (*Display, error) {
	url := baseURL + connectPath
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", url, err)
	}
	return &Display{conn: conn}, nil
}

// Register sends the display_register message and waits for the ack.
// On success it returns a channel that delivers incoming DisplayPayload
// messages. The channel is closed when the connection drops.
func (d *Display) Register(name, displayType string) (<-chan domain.DisplayPayload, error) {
	if err := d.sendRegister(name, displayType); err != nil {
		return nil, err
	}

	id, err := d.readRegisterAck()
	if err != nil {
		return nil, err
	}

	d.ID = id
	d.Name = name

	ch := make(chan domain.DisplayPayload)
	go d.readLoop(ch)
	return ch, nil
}

// Close sends a normal-closure WebSocket message and closes the connection.
func (d *Display) Close() {
	_ = d.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "display shutting down"),
	)
	_ = d.conn.Close()
}

// sendRegister marshals and sends the display_register envelope.
func (d *Display) sendRegister(name, displayType string) error {
	payload, err := json.Marshal(domain.DisplayRegisterPayload{
		Name: name,
		Type: displayType,
	})
	if err != nil {
		return fmt.Errorf("marshal display_register payload: %w", err)
	}

	env := domain.Envelope{
		Type:    domain.MessageTypeDisplayRegister,
		Payload: payload,
	}

	if err := d.conn.WriteJSON(env); err != nil {
		return fmt.Errorf("send display_register: %w", err)
	}

	return nil
}

// readRegisterAck reads the first response and returns the assigned display ID.
func (d *Display) readRegisterAck() (string, error) {
	var resp domain.Envelope
	if err := d.conn.ReadJSON(&resp); err != nil {
		return "", fmt.Errorf("read register response: %w", err)
	}

	switch resp.Type {
	case domain.MessageTypeDisplayRegisterAck:
		var ack domain.DisplayRegisterAckPayload
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			return "", fmt.Errorf("decode display_register_ack: %w", err)
		}
		return ack.DisplayID, nil

	case domain.MessageTypeError:
		var errPayload domain.ErrorPayload
		if err := json.Unmarshal(resp.Payload, &errPayload); err != nil {
			return "", fmt.Errorf("decode error payload: %w", err)
		}
		return "", fmt.Errorf("registration rejected [%s]: %s", errPayload.Code, errPayload.Message)

	default:
		return "", fmt.Errorf("unexpected response type %q", resp.Type)
	}
}

// readLoop pumps incoming messages onto ch until the connection closes.
func (d *Display) readLoop(ch chan<- domain.DisplayPayload) {
	defer close(ch)

	for {
		var msg domain.Envelope
		if err := d.conn.ReadJSON(&msg); err != nil {
			return
		}

		if msg.Type != domain.MessageTypeDisplay {
			continue
		}

		var dp domain.DisplayPayload
		if err := json.Unmarshal(msg.Payload, &dp); err != nil {
			continue
		}

		ch <- dp
	}
}
