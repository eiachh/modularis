package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/internal/ws"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ConnectHandler holds the dependencies required by the /connect endpoint.
type ConnectHandler struct {
	Hub        *ws.Hub
	DisplayHub *ws.Hub
	Registry   *registry.Registry
	Log        *slog.Logger
}

// Handle is the Gin handler for GET /connect.
// It upgrades to WebSocket, waits for a register message, and on success
// adds the agent to the registry and hub.
func (h *ConnectHandler) Handle(c *gin.Context) {
	raw, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Log.Error("websocket upgrade failed", "error", err)
		return
	}

	conn := &ws.Conn{Raw: raw}

	// --- 1. Wait for the first message (must be "register") ---------------

	var env domain.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		h.Log.Error("failed to read first message", "error", err)
		_ = conn.Close()
		return
	}

	if env.Type != domain.MessageTypeRegister {
		h.Log.Warn("first message was not register", "type", env.Type)
		_ = h.sendError(conn, "UNEXPECTED_MESSAGE", fmt.Sprintf("expected %q, got %q", domain.MessageTypeRegister, env.Type))
		_ = conn.Close()
		return
	}

	// --- 2. Decode the register payload -----------------------------------

	var reg domain.RegisterPayload
	if err := json.Unmarshal(env.Payload, &reg); err != nil {
		h.Log.Error("invalid register payload", "error", err)
		_ = h.sendError(conn, "INVALID_PAYLOAD", "could not decode register payload")
		_ = conn.Close()
		return
	}

	if reg.Name == "" {
		_ = h.sendError(conn, "VALIDATION_ERROR", "agent name is required")
		_ = conn.Close()
		return
	}

	// --- 3. Create the agent and persist it --------------------------------

	agent := &domain.Agent{
		ID:           uuid.New().String(),
		Name:         reg.Name,
		Capabilities: reg.Capabilities,
		RegisteredAt: time.Now().UTC(),
	}

	if err := h.Registry.Add(agent); err != nil {
		h.Log.Error("registry add failed", "error", err)
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		_ = conn.Close()
		return
	}

	conn.Metadata = agent
	h.Hub.Register(agent.ID, conn)

	h.Log.Info("agent registered",
		"agent_id", agent.ID,
		"name", agent.Name,
		"capabilities", len(agent.Capabilities),
	)

	// --- 4. Send acknowledgement ------------------------------------------

	if err := conn.SendEnvelope(domain.MessageTypeRegisterAck, domain.RegisterAckPayload{
		AgentID: agent.ID,
	}); err != nil {
		h.Log.Error("failed to send register_ack", "error", err)
		h.cleanup(agent.ID)
		return
	}

	// --- 5. Enter read loop (keep connection alive) -----------------------

	h.readLoop(conn, agent)
}

// readLoop reads messages until the connection drops. It handles "display"
// messages by broadcasting them to all connected display modules.
func (h *ConnectHandler) readLoop(conn *ws.Conn, agent *domain.Agent) {
	defer h.cleanup(agent.ID)

	for {
		var env domain.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				h.Log.Warn("unexpected close", "agent_id", agent.ID, "error", err)
			} else {
				h.Log.Info("agent disconnected", "agent_id", agent.ID)
			}
			return
		}

		switch env.Type {
		case domain.MessageTypeDisplay:
			h.handleDisplayMessage(env.Payload, agent)
		default:
			h.Log.Debug("received message", "agent_id", agent.ID, "type", env.Type)
		}
	}
}

// handleDisplayMessage decodes a display payload from an agent and
// broadcasts it to every connected display module.
func (h *ConnectHandler) handleDisplayMessage(raw json.RawMessage, agent *domain.Agent) {
	var dp domain.DisplayPayload
	if err := json.Unmarshal(raw, &dp); err != nil {
		h.Log.Warn("invalid display payload from agent", "agent_id", agent.ID, "error", err)
		return
	}

	// Stamp the agent identity so displays always know who sent it.
	dp.AgentID = agent.ID
	dp.AgentName = agent.Name

	h.Log.Info("broadcasting display message",
		"agent_id", agent.ID,
		"title", dp.Title,
		"displays", h.DisplayHub.Count(),
	)

	h.DisplayHub.Broadcast(domain.MessageTypeDisplay, dp)
}

// cleanup removes the agent from the hub and registry.
func (h *ConnectHandler) cleanup(agentID string) {
	h.Hub.Unregister(agentID)
	h.Registry.Remove(agentID)
	h.Log.Info("agent cleaned up", "agent_id", agentID)
}

// sendError sends an error envelope to the remote end.
func (h *ConnectHandler) sendError(conn *ws.Conn, code, msg string) error {
	return conn.SendEnvelope(domain.MessageTypeError, domain.ErrorPayload{
		Code:    code,
		Message: msg,
	})
}
