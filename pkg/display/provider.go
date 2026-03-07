package display

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message represents a display message received from the orchestrator.
type Message struct {
	// AgentID identifies which agent produced this output.
	AgentID string `json:"agent_id"`
	// AgentName is the human-readable name of the agent.
	AgentName string `json:"agent_name"`
	// Title is a short summary of what happened.
	Title string `json:"title"`
	// Body is the main content to render.
	Body string `json:"body"`
	// Level indicates severity/importance (info, warn, error, success).
	Level string `json:"level"`
}

// Display provides functionality for a display module to connect to a modularis orchestrator
// and receive messages. It handles the WebSocket connection and reconnection protocol.
type Display struct {
	orchestratorURL string
	displayName      string
	displayType      string
	conn             *websocket.Conn
	mu               sync.Mutex
	messages         chan Message
	closed           chan struct{}
	maxBackoff       time.Duration
}

// New creates a new Display instance configured with the given orchestrator URL,
// display name, display type, and maximum reconnection backoff.
func New(orchestratorURL, displayName, displayType string, maxBackoff time.Duration) *Display {
	return &Display{
		orchestratorURL: orchestratorURL,
		displayName:     displayName,
		displayType:     displayType,
		messages:        make(chan Message, 100),
		closed:          make(chan struct{}, 1),
		maxBackoff:      maxBackoff,
	}
}

// Connect establishes a WebSocket connection to the orchestrator and registers
// the display. It returns the assigned display ID and two channels: one for display messages and one for the close signal.
func (d *Display) Connect() (string, <-chan Message, <-chan struct{}, error) {
	id, err := d.connectAndRegister()
	if err != nil {
		return "", nil, nil, err
	}

	// Start a background goroutine to read messages from the orchestrator
	go d.readLoop()

	return id, d.messages, d.closed, nil
}

func (d *Display) connectAndRegister() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Build WebSocket URL from HTTP URL
	wsURL := d.orchestratorURL
	if len(wsURL) >= 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:]
	}
	wsURL = wsURL + "/display"

	// Establish WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to connect to orchestrator: %w", err)
	}
	d.conn = conn

	// Send display_register message
	registerPayload := map[string]string{
		"name": d.displayName,
		"type": d.displayType,
	}
	registerMsg := envelope{Type: "display_register", Payload: mustMarshal(registerPayload)}
	if err := d.conn.WriteJSON(registerMsg); err != nil {
		d.conn.Close()
		return "", fmt.Errorf("failed to send display_register message: %w", err)
	}

	// Wait for display_register_ack
	var ack envelope
	if err := d.conn.ReadJSON(&ack); err != nil {
		d.conn.Close()
		return "", fmt.Errorf("failed to read display_register_ack: %w", err)
	}

	if ack.Type != "display_register_ack" {
		d.conn.Close()
		return "", fmt.Errorf("unexpected message type: expected display_register_ack, got %s", ack.Type)
	}

	var registerAck struct {
		DisplayID string `json:"display_id"`
	}
	if err := json.Unmarshal(ack.Payload, &registerAck); err != nil {
		d.conn.Close()
		return "", fmt.Errorf("failed to unmarshal display_register_ack: %w", err)
	}

	return registerAck.DisplayID, nil
}

func (d *Display) readLoop() {
	backoff := 1 * time.Second
	for {
		var env envelope
		if err := d.conn.ReadJSON(&env); err != nil {
			// Connection lost or closed - attempt reconnection with backoff
			d.mu.Lock()
			if d.conn != nil {
				d.conn.Close()
			}
			d.mu.Unlock()

			// Notify about connection loss
			select {
			case d.closed <- struct{}{}:
			default:
			}

			// Incremental backoff reconnection loop
			for {
				time.Sleep(backoff)
				if _, err := d.connectAndRegister(); err == nil {
					// Reconnected successfully
					backoff = 1 * time.Second
					break
				}
				backoff *= 2
				if backoff > d.maxBackoff {
					backoff = d.maxBackoff
				}
			}
			continue
		}

		if env.Type == "display" {
			var msg Message
			if err := json.Unmarshal(env.Payload, &msg); err == nil {
				// Non-blocking send
				select {
				case d.messages <- msg:
				default:
				}
			}
		}
	}
}

// Close closes the WebSocket connection to the orchestrator.
func (d *Display) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

// envelope represents a WebSocket message envelope.
type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// mustMarshal is a helper that panics on marshal error (for static data).
func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}