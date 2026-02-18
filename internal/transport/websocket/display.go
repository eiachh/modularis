package websocket

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/modularis/modularis/internal/application/agent"
	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/hub"
	"github.com/modularis/modularis/internal/registry"
)

// DisplayHandler is thin transport for display WS /display: upgrades, decodes,
// establishes conn, passes to hub.Register (hub stores/calls all WS conns globally).
// No biz logic; delegates minimal to service/agent hub if needed.
type DisplayHandler struct {
	Service         *agent.Service // for any agent overlap
	DisplayHub      *hub.Hub
	DisplayRegistry *registry.DisplayRegistry
	Log             *slog.Logger
}

// Handle upgrades WS, reads display_register, delegates to service, sends ack,
// enters read loop.
func (h *DisplayHandler) Handle(c *gin.Context) {
	raw, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Log.Error("websocket upgrade failed", "error", err)
		return
	}

	conn := &hub.Conn{Raw: raw}

	// Decode envelope (must be display_register).
	var env domain.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		h.Log.Error("failed to read first message", "error", err)
		_ = conn.Close()
		return
	}

	if env.Type != domain.MessageTypeDisplayRegister {
		_ = h.sendError(conn, "UNEXPECTED_MESSAGE", "expected display_register")
		_ = conn.Close()
		return
	}

	// Decode payload.
	var reg domain.DisplayRegisterPayload
	if err := json.Unmarshal(env.Payload, &reg); err != nil {
		_ = h.sendError(conn, "INVALID_PAYLOAD", "decode failed")
		_ = conn.Close()
		return
	}

	// Handler establishes display (original style): create, persist to registry,
	// pass full conn to hub.Register (hub stores/calls conns globally).
	display := &domain.Display{
		ID:           uuid.New().String(),
		Name:         reg.Name,
		Type:         reg.Type,
		RegisteredAt: time.Now().UTC(),
	}
	if err := h.DisplayRegistry.Add(display); err != nil { // assume DisplayRegistry injected or global hub
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		_ = conn.Close()
		return
	}

	conn.Metadata = display
	h.DisplayHub.Register(display.ID, conn) // pass conn to hub (storage/calls)

	// Send ack.
	if err := conn.SendEnvelope(domain.MessageTypeDisplayRegisterAck, domain.DisplayRegisterAckPayload{DisplayID: display.ID}); err != nil {
		// cleanup via hub
		return
	}

	// Read loop (decode only; display mostly receive-only).
	h.readLoop(conn, display.ID)
}

// readLoop keeps display conn alive, delegates any messages.
func (h *DisplayHandler) readLoop(conn *hub.Conn, displayID string) {
	// No cleanup in service for display yet; simple.
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
		// delegate if needed
	}
}

// sendError transport helper.
func (h *DisplayHandler) sendError(conn *hub.Conn, code, msg string) error {
	return conn.SendEnvelope(domain.MessageTypeError, domain.ErrorPayload{
		Code:    code,
		Message: msg,
	})
}
