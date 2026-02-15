package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/internal/ws"
)

// DisplayHandler holds the dependencies required by the /display endpoint.
type DisplayHandler struct {
	DisplayHub      *ws.Hub
	DisplayRegistry *registry.DisplayRegistry
	Log             *slog.Logger
}

// Handle is the Gin handler for GET /display.
// It upgrades to WebSocket, waits for a display_register message, and on
// success adds the display to the registry and hub.
func (h *DisplayHandler) Handle(c *gin.Context) {
	raw, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Log.Error("websocket upgrade failed", "error", err)
		return
	}

	conn := &ws.Conn{Raw: raw}

	// --- 1. Wait for the first message (must be "display_register") -------

	var env domain.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		h.Log.Error("failed to read first message", "error", err)
		_ = conn.Close()
		return
	}

	if env.Type != domain.MessageTypeDisplayRegister {
		h.Log.Warn("first message was not display_register", "type", env.Type)
		_ = h.sendError(conn, "UNEXPECTED_MESSAGE", fmt.Sprintf("expected %q, got %q", domain.MessageTypeDisplayRegister, env.Type))
		_ = conn.Close()
		return
	}

	// --- 2. Decode the display register payload ---------------------------

	var reg domain.DisplayRegisterPayload
	if err := json.Unmarshal(env.Payload, &reg); err != nil {
		h.Log.Error("invalid display register payload", "error", err)
		_ = h.sendError(conn, "INVALID_PAYLOAD", "could not decode display_register payload")
		_ = conn.Close()
		return
	}

	if reg.Name == "" {
		_ = h.sendError(conn, "VALIDATION_ERROR", "display name is required")
		_ = conn.Close()
		return
	}

	if reg.Type == "" {
		_ = h.sendError(conn, "VALIDATION_ERROR", "display type is required")
		_ = conn.Close()
		return
	}

	// --- 3. Create the display and persist it ------------------------------

	display := &domain.Display{
		ID:           uuid.New().String(),
		Name:         reg.Name,
		Type:         reg.Type,
		RegisteredAt: time.Now().UTC(),
	}

	if err := h.DisplayRegistry.Add(display); err != nil {
		h.Log.Error("display registry add failed", "error", err)
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		_ = conn.Close()
		return
	}

	conn.Metadata = display
	h.DisplayHub.Register(display.ID, conn)

	h.Log.Info("display registered",
		"display_id", display.ID,
		"name", display.Name,
		"type", display.Type,
	)

	// --- 4. Send acknowledgement ------------------------------------------

	if err := conn.SendEnvelope(domain.MessageTypeDisplayRegisterAck, domain.DisplayRegisterAckPayload{
		DisplayID: display.ID,
	}); err != nil {
		h.Log.Error("failed to send display_register_ack", "error", err)
		h.cleanup(display.ID)
		return
	}

	// --- 5. Enter read loop (keep connection alive) -----------------------

	h.readLoop(conn, display.ID)
}

// readLoop reads messages until the connection drops. Display connections
// are mostly receive-only but the loop keeps the connection alive.
func (h *DisplayHandler) readLoop(conn *ws.Conn, displayID string) {
	defer h.cleanup(displayID)

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

// cleanup removes the display from the hub and registry.
func (h *DisplayHandler) cleanup(displayID string) {
	h.DisplayHub.Unregister(displayID)
	h.DisplayRegistry.Remove(displayID)
	h.Log.Info("display cleaned up", "display_id", displayID)
}

// sendError sends an error envelope to the remote end.
func (h *DisplayHandler) sendError(conn *ws.Conn, code, msg string) error {
	return conn.SendEnvelope(domain.MessageTypeError, domain.ErrorPayload{
		Code:    code,
		Message: msg,
	})
}