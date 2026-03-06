package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/service"
	"github.com/modularis/modularis/internal/ws"
)

// DisplayHandler manages the display WebSocket lifecycle.
type DisplayHandler struct {
	Service *service.DisplayService
	Log     *slog.Logger
}

// Handle upgrades the connection, registers the display, then enters the read loop.
func (h *DisplayHandler) Handle(c *gin.Context) {
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
		_ = h.sendError(conn, "INVALID_PAYLOAD", "could not decode display_register payload")
		h.closeConn(conn)
		return
	}

	if reg.Name == "" {
		_ = h.sendError(conn, "VALIDATION_ERROR", "display name is required")
		h.closeConn(conn)
		return
	}
	if reg.Type == "" {
		_ = h.sendError(conn, "VALIDATION_ERROR", "display type is required")
		h.closeConn(conn)
		return
	}

	display, err := h.Service.Register(conn, reg.Name, reg.Type)
	if err != nil {
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		h.closeConn(conn)
		return
	}

	if err := conn.SendEnvelope(domain.MessageTypeDisplayRegisterAck, domain.DisplayRegisterAckPayload{DisplayID: display.ID}); err != nil {
		h.Log.Error("failed to send display_register_ack", "error", err)
		h.Service.Deregister(display.ID)
		h.closeConn(conn)
		return
	}

	h.readLoop(conn, display.ID)
}

// readLoop keeps the display connection alive until it disconnects.
func (h *DisplayHandler) readLoop(conn *ws.Conn, displayID string) {
	defer h.Service.Deregister(displayID)

	for {
		var env domain.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				h.Log.Warn("unexpected display close", "display_id", displayID, "error", err)
			} else {
				h.Log.Info("display disconnected", "display_id", displayID)
			}
			return
		}
		h.Log.Debug("received message from display", "display_id", displayID, "type", env.Type)
	}
}

func (h *DisplayHandler) upgradeConnection(c *gin.Context) (*ws.Conn, error) {
	raw, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Log.Error("websocket upgrade failed", "error", err)
		return nil, err
	}
	return &ws.Conn{Raw: raw}, nil
}

func (h *DisplayHandler) readFirstEnvelope(conn *ws.Conn) (domain.Envelope, error) {
	var env domain.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		h.Log.Error("failed to read first message", "error", err)
		return domain.Envelope{}, err
	}
	if env.Type != domain.MessageTypeDisplayRegister {
		h.Log.Warn("first message was not display_register", "type", env.Type)
		_ = h.sendError(conn, "UNEXPECTED_MESSAGE", fmt.Sprintf("expected %q, got %q", domain.MessageTypeDisplayRegister, env.Type))
		return domain.Envelope{}, fmt.Errorf("unexpected first message type %q", env.Type)
	}
	return env, nil
}

func (h *DisplayHandler) decodeRegisterPayload(env domain.Envelope) (domain.DisplayRegisterPayload, error) {
	var reg domain.DisplayRegisterPayload
	if err := json.Unmarshal(env.Payload, &reg); err != nil {
		h.Log.Error("invalid display register payload", "error", err)
		return domain.DisplayRegisterPayload{}, err
	}
	return reg, nil
}

func (h *DisplayHandler) closeConn(conn *ws.Conn) {
	if conn != nil {
		_ = conn.Close()
	}
}

// sendError replies with an error envelope.
func (h *DisplayHandler) sendError(conn *ws.Conn, code, msg string) error {
	return conn.SendEnvelope(domain.MessageTypeError, domain.ErrorPayload{
		Code:    code,
		Message: msg,
	})
}