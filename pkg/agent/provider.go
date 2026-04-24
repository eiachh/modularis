package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/eiachh/Modularis/pkg/protocol"
	"github.com/gorilla/websocket"
)

// Invocation represents a capability that an agent can register,
// or an invocation request from the orchestrator.
type Invocation struct {
	// ID is the unique capability/invocation ID assigned by the orchestrator.
	// Present on invocations received from the orchestrator; empty for registrations.
	ID string `json:"id,omitempty"`
	// Name is the unique identifier for this capability (e.g., "echo", "scream").
	Name string `json:"name"`
	// Args is the JSON Schema describing the expected input arguments (for registration)
	// or the actual arguments (for invocation).
	Args json.RawMessage `json:"args"`
}

// Agent provides functionality for an agent to connect to a modularis orchestrator
// and register its capabilities. It handles the WebSocket connection and
// capability registration protocol.
type Agent struct {
	orchestratorURL string
	agentName       string
	capabilities    []Invocation
	conn            *websocket.Conn
	mu              sync.Mutex
	invocations     chan Invocation
	closed          chan struct{}
	done            chan struct{}
	doneOnce        sync.Once
	maxBackoff      time.Duration

	// handlers maps capability function names to their handler functions.
	// When a command arrives for a capability with a handler, the handler is
	// invoked and its result is sent back as a command_result message.
	handlers map[string]func(json.RawMessage) json.RawMessage
}

const defaultOrchestratorURL = "http://localhost:8080"

// New creates a new Agent instance configured with the given orchestrator URL,
// agent name, and maximum reconnection backoff.
// Use AddCapability to register capabilities before calling Connect.
// If orchestratorURL is empty, falls back to ORCHESTRATOR_URL env or default.
func New(orchestratorURL, agentName string, maxBackoff time.Duration) *Agent {
	if orchestratorURL == "" {
		if v := os.Getenv("ORCHESTRATOR_URL"); v != "" {
			orchestratorURL = v
		} else {
			orchestratorURL = defaultOrchestratorURL
		}
	}
	return &Agent{
		orchestratorURL: orchestratorURL,
		agentName:       agentName,
		capabilities:    make([]Invocation, 0),
		invocations:     make(chan Invocation, 100),
		closed:          make(chan struct{}, 1),
		done:            make(chan struct{}),
		maxBackoff:      maxBackoff,
		handlers:        make(map[string]func(json.RawMessage) json.RawMessage),
	}
}

// AddCapability registers a capability schema with the agent.
// This should be called before Connect to ensure all capabilities are registered
// upon connection to the orchestrator.
//
// Capabilities registered with AddCapability are fire-and-forget: invocations
// are delivered via the invocations channel returned by Connect(); the agent
// application is responsible for processing and responding (e.g., via display).
func (a *Agent) AddCapability(name string, schema json.RawMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.capabilities = append(a.capabilities, Invocation{
		Name: name,
		Args: schema,
	})
}

// AddCapabilityWithHandler registers a capability with a handler function.
// When the orchestrator sends a command for this capability, the handler is
// invoked with the raw args JSON and its return value (raw result JSON) is
// sent back to the orchestrator as a command_result message.
//
// This enables a request-response pattern: the caller can correlate the result
// using the capability_id from the invocation.
//
// The handler receives the invocation args and should return the result payload.
// Returning nil or an empty slice is valid (no result data).
func (a *Agent) AddCapabilityWithHandler(name string, schema json.RawMessage, handler func(args json.RawMessage) json.RawMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.capabilities = append(a.capabilities, Invocation{
		Name: name,
		Args: schema,
	})
	a.handlers[name] = handler
}

// Connect establishes a WebSocket connection to the orchestrator and registers
// the agent with all previously added capabilities. It sends the initial register
// message, waits for acknowledgment, then registers each capability.
// It returns the assigned agent ID and two channels: one for invocation requests and one for the close signal.
func (a *Agent) Connect() (string, <-chan Invocation, <-chan struct{}, error) {
	id, err := a.connectAndRegister()
	if err != nil {
		return "", nil, nil, err
	}

	// Start a background goroutine to read messages from the orchestrator
	go a.readLoop()

	return id, a.invocations, a.closed, nil
}

func (a *Agent) connectAndRegister() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Build WebSocket URL from HTTP URL
	wsURL := a.orchestratorURL
	if len(wsURL) >= 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:]
	}
	wsURL = wsURL + "/connect"

	// Establish WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to connect to orchestrator: %w", err)
	}
	a.conn = conn

	// Send register message
	registerPayload := map[string]string{"name": a.agentName}
	registerMsg := protocol.Envelope{Type: "register", Payload: protocol.MustMarshal(registerPayload)}
	if err := a.conn.WriteJSON(registerMsg); err != nil {
		a.conn.Close()
		return "", fmt.Errorf("failed to send register message: %w", err)
	}

	// Wait for register_ack
	var ack protocol.Envelope
	if err := a.conn.ReadJSON(&ack); err != nil {
		a.conn.Close()
		return "", fmt.Errorf("failed to read register_ack: %w", err)
	}

	if ack.Type != "register_ack" {
		a.conn.Close()
		return "", fmt.Errorf("unexpected message type: expected register_ack, got %s", ack.Type)
	}

	var registerAck struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(ack.Payload, &registerAck); err != nil {
		a.conn.Close()
		return "", fmt.Errorf("failed to unmarshal register_ack: %w", err)
	}

	// Register all capabilities
	for _, cap := range a.capabilities {
		capPayload := map[string]any{
			"agent_name":    a.agentName,
			"function_name": cap.Name,
			"schema":        cap.Args,
		}
		capMsg := protocol.Envelope{Type: "capability_register", Payload: protocol.MustMarshal(capPayload)}
		if err := a.conn.WriteJSON(capMsg); err != nil {
			a.conn.Close()
			return "", fmt.Errorf("failed to register capability %s: %w", cap.Name, err)
		}

		// Wait for capability_register_ack
		var capAck protocol.Envelope
		if err := a.conn.ReadJSON(&capAck); err != nil {
			a.conn.Close()
			return "", fmt.Errorf("failed to read capability_register_ack: %w", err)
		}

		if capAck.Type != "capability_register_ack" {
			a.conn.Close()
			return "", fmt.Errorf("unexpected message type: expected capability_register_ack, got %s", capAck.Type)
		}
	}

	return registerAck.AgentID, nil
}

func (a *Agent) readLoop() {
	backoff := 1 * time.Second
	for {
		select {
		case <-a.done:
			return
		default:
		}

		var env protocol.Envelope
		if err := a.conn.ReadJSON(&env); err != nil {
			// Check if we were asked to stop before reconnecting
			select {
			case <-a.done:
				return
			default:
			}

			// Connection lost or closed - attempt reconnection with backoff
			a.mu.Lock()
			if a.conn != nil {
				a.conn.Close()
			}
			a.mu.Unlock()

			// Notify about connection loss
			select {
			case a.closed <- struct{}{}:
			default:
			}

			// Incremental backoff reconnection loop
			for {
				select {
				case <-a.done:
					return
				case <-time.After(backoff):
				}
				if _, err := a.connectAndRegister(); err == nil {
					backoff = 1 * time.Second
					break
				}
				backoff *= 2
				if backoff > a.maxBackoff {
					backoff = a.maxBackoff
				}
			}
			continue
		}

		if env.Type == "command" {
			var inv struct {
				CapabilityID string          `json:"capability_id"`
				FunctionName string          `json:"function_name"`
				Args         json.RawMessage `json:"args"`
			}
			if err := json.Unmarshal(env.Payload, &inv); err == nil {
				// Check if a handler is registered for this capability.
				a.mu.Lock()
				handler, hasHandler := a.handlers[inv.FunctionName]
				a.mu.Unlock()

				if hasHandler {
					// Request-response mode: invoke handler and send result back.
					var result json.RawMessage
					func() {
						defer func() {
							if r := recover(); r != nil {
								// Handler panicked; send error result
								result, _ = json.Marshal(map[string]any{"error": fmt.Sprintf("panic: %v", r)})
							}
						}()
						result = handler(inv.Args)
					}()
					// Send command_result to orchestrator
					a.sendCommandResult(inv.CapabilityID, result)
				} else {
					// Fire-and-forget mode: deliver via channel for app to handle.
					cmd := Invocation{
						ID:   inv.CapabilityID,
						Name: inv.FunctionName,
						Args: inv.Args,
					}
					select {
					case a.invocations <- cmd:
					default:
					}
				}
			}
		}
	}
}

// sendCommandResult sends a command_result message back to the orchestrator
// with the given capability ID and result payload.
func (a *Agent) sendCommandResult(capabilityID string, result json.RawMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn == nil {
		return
	}

	payload := map[string]any{
		"capability_id": capabilityID,
		"result":        result,
	}
	msg := protocol.Envelope{Type: "command_result", Payload: protocol.MustMarshal(payload)}
	_ = a.conn.WriteJSON(msg)
}

// Close signals the readLoop to stop and closes the WebSocket connection.
func (a *Agent) Close() error {
	a.doneOnce.Do(func() { close(a.done) })

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn != nil {
		return a.conn.Close()
	}
	return nil
}

// SendCommandResult sends a command_result message back to the orchestrator
// for a fire-and-forget capability (one registered via AddCapability).
// The result can be nil for simple acks. This is called automatically
// for capabilities registered with AddCapabilityWithHandler.
func (a *Agent) SendCommandResult(capabilityID string, result json.RawMessage) {
	a.sendCommandResult(capabilityID, result)
}

// SendDisplay sends a display message to the orchestrator to be broadcast
// to all connected display modules.
func (a *Agent) SendDisplay(title, body, level string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn == nil {
		return
	}

	payload := map[string]any{
		"title": title,
		"body":  body,
		"level": level,
	}
	msg := protocol.Envelope{Type: "display", Payload: protocol.MustMarshal(payload)}
	_ = a.conn.WriteJSON(msg)
}

// Run connects to the orchestrator and runs the event loop, blocking until
// ctx is cancelled. The handler is called for each fire-and-forget invocation
// (capabilities registered with AddCapabilityWithHandler are handled
// automatically and do not reach the handler). Returns the assigned agent ID.
func (a *Agent) Run(ctx context.Context, handler func(Invocation)) (string, error) {
	id, invocations, closed, err := a.Connect()
	if err != nil {
		return "", err
	}

	for {
		select {
		case <-ctx.Done():
			a.Close()
			return id, nil
		case inv, ok := <-invocations:
			if !ok {
				return id, nil
			}
			handler(inv)
		case _, ok := <-closed:
			if !ok {
				return id, nil
			}
		}
	}
}