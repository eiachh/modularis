package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/modularis/modularis/internal/application/agent"
	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/hub"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// AgentHandler is thin transport layer for agent WS /connect: upgrades,
// decodes messages, delegates to application service, sends responses.
// No business logic here.
type AgentHandler struct {
	Service *agent.Service
	Log     *slog.Logger
}

// Handle upgrades WS, reads register, delegates registration to service,
// sends ack, enters read loop delegating further messages.
func (h *AgentHandler) Handle(c *gin.Context) {
	raw, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Log.Error("websocket upgrade failed", "error", err)
		return
	}

	conn := &hub.Conn{Raw: raw}

	// Decode first envelope (must be register).
	var env domain.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		h.Log.Error("failed to read first message", "error", err)
		_ = conn.Close()
		return
	}

	if env.Type != domain.MessageTypeRegister {
		h.Log.Warn("first message was not register", "type", env.Type)
		_ = h.sendError(conn, "UNEXPECTED_MESSAGE", "expected register")
		_ = conn.Close()
		return
	}

	// Decode register payload.
	var reg domain.RegisterPayload
	if err := json.Unmarshal(env.Payload, &reg); err != nil {
		_ = h.sendError(conn, "INVALID_PAYLOAD", "decode failed")
		_ = conn.Close()
		return
	}

	// Delegate to service for registration (biz only; no conn).
	ag, ack, err := h.Service.RegisterAgent(reg.Name)
	if err != nil {
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		_ = conn.Close()
		return
	}

	// Set metadata and register full conn to hub (prevents nil deref in
	// Get/Broadcast; transport responsibility).
	conn.Metadata = ag
	h.Service.AgentHub.Register(ag.ID, conn) // note: service exposes hub for transport

	// Send ack.
	if err := conn.SendEnvelope(domain.MessageTypeRegisterAck, ack); err != nil {
		h.Service.CleanupAgent(ag.ID)
		return
	}

	// Enter read loop (decode + delegate).
	h.readLoop(conn, ag)
}

// readLoop decodes incoming WS messages and delegates to service.
func (h *AgentHandler) readLoop(conn *hub.Conn, agent *domain.Agent) {
	defer h.Service.CleanupAgent(agent.ID)

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
			h.Service.ProcessDisplayMessage(env.Payload, agent)
		case domain.MessageTypeCapabilityRegister:
			// Delegate; send ack/error via conn if needed (extend service if req).
			_ = h.Service.RegisterCapability(env.Payload, agent) // ignore err for loop
		default:
			_ = h.sendError(conn, "UNEXPECTED_MESSAGE", "unsupported type")
		}
	}
}

// sendError is transport helper for error envelope.
func (h *AgentHandler) sendError(conn *hub.Conn, code, msg string) error {
	return conn.SendEnvelope(domain.MessageTypeError, domain.ErrorPayload{
		Code:    code,
		Message: msg,
	})
}
