package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/service"
	"github.com/modularis/modularis/internal/ws"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// AgentHandler manages the agent WebSocket lifecycle.
type AgentHandler struct {
	Service *service.AgentService
	Log     *slog.Logger
}

// Handle upgrades the connection, registers the agent, then enters the read loop.
func (h *AgentHandler) Handle(c *gin.Context) {
	conn, err := h.upgradeConnection(c)
	if err != nil {
		return
	}

	env, err := h.readFirstEnvelope(conn)
	if err != nil {
		h.closeConn(conn)
		return
	}

	reg, err := h.decodeRegisterPayload(env)
	if err != nil {
		_ = h.sendError(conn, "INVALID_PAYLOAD", "could not decode register payload")
		h.closeConn(conn)
		return
	}

	if reg.Name == "" {
		_ = h.sendError(conn, "VALIDATION_ERROR", "agent name is required")
		h.closeConn(conn)
		return
	}

	agent, err := h.Service.Register(conn, reg.Name)
	if err != nil {
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		h.closeConn(conn)
		return
	}

	if err := conn.SendEnvelope(domain.MessageTypeRegisterAck, domain.RegisterAckPayload{AgentID: agent.ID}); err != nil {
		h.Log.Error("failed to send register_ack", "error", err)
		h.Service.Deregister(agent.ID)
		h.closeConn(conn)
		return
	}

	h.readLoop(conn, agent)
}

// readLoop processes incoming messages until the agent disconnects.
func (h *AgentHandler) readLoop(conn *ws.Conn, agent *domain.Agent) {
	defer h.Service.Deregister(agent.ID)

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
			h.Service.BroadcastDisplay(agent, env.Payload)
		case domain.MessageTypeCapabilityRegister:
			h.handleCapabilityRegister(conn, agent, env.Payload)
		default:
			h.Log.Warn("unknown message type", "agent_id", agent.ID, "type", env.Type)
			_ = h.sendError(conn, "UNEXPECTED_MESSAGE", fmt.Sprintf("unsupported message type %q", env.Type))
		}
	}
}

// handleCapabilityRegister delegates to the service and sends the ack.
func (h *AgentHandler) handleCapabilityRegister(conn *ws.Conn, agent *domain.Agent, raw json.RawMessage) {
	cap, err := h.Service.RegisterCapability(agent, raw)
	if err != nil {
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		return
	}

	if err := conn.SendEnvelope(domain.MessageTypeCapabilityRegisterAck, domain.CapabilityRegisterAckPayload{
		CapabilityName: cap.Name,
	}); err != nil {
		h.Log.Error("failed to send capability_register_ack", "agent_id", agent.ID, "error", err)
	}
}

func (h *AgentHandler) upgradeConnection(c *gin.Context) (*ws.Conn, error) {
	raw, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Log.Error("websocket upgrade failed", "error", err)
		return nil, err
	}
	return &ws.Conn{Raw: raw}, nil
}

func (h *AgentHandler) readFirstEnvelope(conn *ws.Conn) (domain.Envelope, error) {
	var env domain.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		h.Log.Error("failed to read first message", "error", err)
		return domain.Envelope{}, err
	}
	if env.Type != domain.MessageTypeRegister {
		h.Log.Warn("first message was not register", "type", env.Type)
		_ = h.sendError(conn, "UNEXPECTED_MESSAGE", fmt.Sprintf("expected %q, got %q", domain.MessageTypeRegister, env.Type))
		return domain.Envelope{}, fmt.Errorf("unexpected first message type %q", env.Type)
	}
	return env, nil
}

func (h *AgentHandler) decodeRegisterPayload(env domain.Envelope) (domain.RegisterPayload, error) {
	var reg domain.RegisterPayload
	if err := json.Unmarshal(env.Payload, &reg); err != nil {
		h.Log.Error("invalid register payload", "error", err)
		return domain.RegisterPayload{}, err
	}
	return reg, nil
}

func (h *AgentHandler) closeConn(conn *ws.Conn) {
	if conn != nil {
		_ = conn.Close()
	}
}

// sendError replies with an error envelope.
func (h *AgentHandler) sendError(conn *ws.Conn, code, msg string) error {
	return conn.SendEnvelope(domain.MessageTypeError, domain.ErrorPayload{
		Code:    code,
		Message: msg,
	})
}

