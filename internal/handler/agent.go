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

	// domain for internal types (e.g., Agent, Capability, CommandPayload).
	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/internal/ws"
	// pkg for shared public types (InvokeCommand, CapabilitySummary,
	// CommandResultPayload) - follows Go conventions, exportable for
	// third parties/clients.
	"github.com/modularis/modularis/pkg"
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
	//
	// Note: Capabilities are no longer accepted here (no legacy support).
	// All capabilities must be registered at runtime via capability_register
	// after WebSocket upgrade for dynamic component loading.

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
	//
	// Agents start with an empty capabilities list; runtime registration
	// (via /agent/capability/register) will populate via the registry.

	agent := &domain.Agent{
		ID:           uuid.New().String(),
		Name:         reg.Name,
		Capabilities: nil, // empty until runtime registration
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
		"initial_capabilities", len(agent.Capabilities),
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

// readLoop reads messages until the connection drops. It now also handles
// "capability_register" messages sent by agents after initial registration
// (the runtime /agent/capability/register path over the established WebSocket)
// as well as "display" messages. This supports dynamic capability updates
// when agent components are loaded at runtime.
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
		case domain.MessageTypeCapabilityRegister:
			// Handle runtime capability registration. Pass conn so we can
			// send ack or error back over the same WebSocket.
			h.handleCapabilityRegister(env.Payload, agent, conn)
		default:
			// Unknown message types are logged and rejected with an error
			// envelope to aid debugging and prevent silent failures.
			h.Log.Warn("unknown message type", "agent_id", agent.ID, "type", env.Type)
			_ = h.sendError(conn, "UNEXPECTED_MESSAGE", fmt.Sprintf("unsupported message type %q", env.Type))
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

// handleCapabilityRegister processes a MessageTypeCapabilityRegister payload
// sent by an agent after initial registration and WebSocket upgrade. This is
// the implementation of the requested /agent/capability/register path.
//
// The agent presents:
//   - agent_name
//   - function_name
//   - schema (JSON Schema containing the capability's required arguments)
//
// The orchestrator:
//   1. Validates the payload.
//   2. Constructs a domain.Capability using the new InputSchema field.
//   3. Calls registry.RegisterCapability (keyed by agent ID for reliability).
//   4. Sends a capability_register_ack on success or error envelope on failure.
//
// This satisfies runtime/dynamic capability registration while components
// and functions can be loaded/changed without reconnecting.
func (h *ConnectHandler) handleCapabilityRegister(raw json.RawMessage, agent *domain.Agent, conn *ws.Conn) {
	var crp domain.CapabilityRegisterPayload
	if err := json.Unmarshal(raw, &crp); err != nil {
		h.Log.Error("invalid capability_register payload", "agent_id", agent.ID, "error", err)
		_ = h.sendError(conn, "INVALID_PAYLOAD", "could not decode capability_register payload")
		return
	}

	if crp.FunctionName == "" {
		_ = h.sendError(conn, "VALIDATION_ERROR", "function_name is required")
		return
	}
	if len(crp.Schema) == 0 {
		_ = h.sendError(conn, "VALIDATION_ERROR", "schema is required")
		return
	}

	// Optional verification: ensure agent_name matches the registered name
	// if the field was populated (helps catch misbehaving agents).
	if crp.AgentName != "" && crp.AgentName != agent.Name {
		h.Log.Warn("agent_name mismatch in capability registration",
			"agent_id", agent.ID,
			"expected", agent.Name,
			"got", crp.AgentName,
		)
		// Continue anyway - ID from connection is authoritative.
	}

	// Build capability. Description is auto-generated because the
	// capability_register payload (as specified) does not include a
	// description field; it focuses on name + schema. Initial register
	// messages can still supply full Capability structs with descriptions.
	cap := domain.Capability{
		Name:        crp.FunctionName,
		Description: fmt.Sprintf("Runtime-registered capability %q", crp.FunctionName),
		InputSchema: crp.Schema,
	}

	// Persist/update in registry (thread-safe, supports update-in-place).
	// This ensures that any client querying agent capabilities (e.g. via
	// future REST endpoints) sees the change immediately.
	if err := h.Registry.RegisterCapability(agent.ID, cap); err != nil {
		h.Log.Error("capability registration failed",
			"agent_id", agent.ID,
			"capability", cap.Name,
			"error", err,
		)
		_ = h.sendError(conn, "REGISTRATION_FAILED", err.Error())
		return
	}

	h.Log.Info("capability registered at runtime",
		"agent_id", agent.ID,
		"name", agent.Name,
		"capability", cap.Name,
	)

	// --- Send acknowledgement ------------------------------------------
	//
	// Mirrors the register_ack pattern used at connect time.
	if err := conn.SendEnvelope(domain.MessageTypeCapabilityRegisterAck, domain.CapabilityRegisterAckPayload{
		CapabilityName: cap.Name,
	}); err != nil {
		h.Log.Error("failed to send capability_register_ack", "agent_id", agent.ID, "error", err)
		// Do not tear down the connection for an ack send failure;
		// the capability was still registered.
		return
	}
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

// CapabilitiesHandler holds dependencies for agent capability endpoints:
// GET /capabilities (list) and POST /invoke (forward to agent WS).
// Includes agentHub for routing commands to connected agents.
type CapabilitiesHandler struct {
	Registry *registry.Registry
	// Hub allows sending commands to specific agent WS connections.
	Hub *ws.Hub
	Log *slog.Logger
}

// Handle is the Gin handler for GET /capabilities.
// Returns all registered capabilities in the exact format specified:
//   [
//     {
//       "agent_name": "...",
//       "function_name": "...",
//       "schema": {json-schema for required args}
//     },
//     ...
//   ]
// This aggregates runtime-registered caps (via capability_register) across
// all agents. No legacy support for initial register caps.
func (h *CapabilitiesHandler) Handle(c *gin.Context) {
	// Fetch current snapshot (reflects any runtime changes).
	agents := h.Registry.List()

	// Flatten to domain.CapabilitySummary.
	var summaries []domain.CapabilitySummary
	for _, a := range agents {
		for _, cap := range a.Capabilities {
			// Include only fully-registered runtime capabilities (schema
			// present by design in capability_register flow).
			summaries = append(summaries, domain.CapabilitySummary{
				AgentName:    a.Name,
				FunctionName: cap.Name,
				Schema:       cap.InputSchema,
			})
		}
	}

	h.Log.Info("served capabilities list",
		"agents", len(agents),
		"total_capabilities", len(summaries),
	)

	// Return as JSON array (200 OK). Gin handles serialization.
	c.JSON(http.StatusOK, summaries)
}

// HandleInvoke is the Gin handler for POST /invoke.
// Receives InvokeCommand (shared package from pkg/) from client, resolves
// agent, *validates args against the capability's JSON schema* (server-side
// using pkg.ValidateArgsAgainstSchema for defense-in-depth), then forwards
// via WS hub to the agent's connection (only "echo" supported: agent
// processes and broadcasts to display). Returns domain.CommandResultPayload.
// Uses pkg.InvokeCommand (defined once in pkg/ for third-party reuse - no
// dups with internal structs).
func (h *CapabilitiesHandler) HandleInvoke(c *gin.Context) {
	// Bind to shared InvokeCommand from pkg/ (the struct moved to pkg
	// as requested for third-party use; matches client assembly + OpenAPI).
	var req pkg.InvokeCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		h.Log.Warn("invalid invoke request", "error", err)
		// Use domain.CommandResultPayload for error (internal); InvokeCommand
		// is the only type in pkg for public API.
		c.JSON(http.StatusBadRequest, domain.CommandResultPayload{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Lookup agent by name (for routing; GetByName added to registry).
	agent := h.Registry.GetByName(req.AgentName)
	if agent == nil {
		h.Log.Warn("agent not found for invoke", "agent_name", req.AgentName)
		c.JSON(http.StatusNotFound, domain.CommandResultPayload{
			Success: false,
			Error:   fmt.Sprintf("agent %q not registered", req.AgentName),
		})
		return
	}

	// Verify function exists in agent's runtime caps and capture schema for
	// validation (only "echo" currently supported/registered).
	// Copy value to avoid range var gotcha.
	var foundCap *domain.Capability
	for i := range agent.Capabilities {
		if agent.Capabilities[i].Name == req.FunctionName {
			cp := agent.Capabilities[i] // copy
			foundCap = &cp
			break
		}
	}
	if foundCap == nil {
		h.Log.Warn("function not found", "function", req.FunctionName, "agent", req.AgentName)
		c.JSON(http.StatusNotFound, domain.CommandResultPayload{
			Success: false,
			Error:   fmt.Sprintf("function %q not registered on agent", req.FunctionName),
		})
		return
	}

	// Debug to confirm schema for val (ensures InputSchema present from reg).
	h.Log.Debug("found cap for val",
		"function", req.FunctionName,
		"schema_len", len(foundCap.InputSchema),
	)

	// Server-side schema validation using shared pkg util (defense-in-depth:
	// re-validates InvokeCommand args even if client did; uses schema from
	// runtime registration). Passes RawMessage directly (no re-marshal).
	h.Log.Debug("server-side schema val",
		"function", req.FunctionName,
		"args", string(req.Args),
		"schema_len", len(foundCap.InputSchema),
	)
	if err := pkg.ValidateArgsAgainstSchema(req.Args, foundCap.InputSchema); err != nil {
		h.Log.Warn("schema validation failed on invoke",
			"function", req.FunctionName,
			"agent", req.AgentName,
			"error", err,
		)
		c.JSON(http.StatusBadRequest, domain.CommandResultPayload{
			Success: false,
			Error:   fmt.Sprintf("invalid args for %q: %v", req.FunctionName, err),
		})
		return
	}

	// Build internal command payload for WS forward (fill ID).
	cmdPayload := domain.CommandPayload{
		AgentID:      agent.ID,
		AgentName:    req.AgentName,
		FunctionName: req.FunctionName,
		Args:         req.Args,
	}

	// Get WS conn from hub and send (orchestrator → agent).
	agentConn := h.Hub.Get(agent.ID)
	if agentConn == nil {
		h.Log.Error("agent not connected in hub", "agent_id", agent.ID)
		c.JSON(http.StatusServiceUnavailable, domain.CommandResultPayload{
			Success: false,
			Error:   "agent WS connection lost",
		})
		return
	}
	if err := agentConn.SendEnvelope(domain.MessageTypeCommand, cmdPayload); err != nil {
		h.Log.Error("failed to forward command to agent", "error", err)
		c.JSON(http.StatusInternalServerError, domain.CommandResultPayload{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	h.Log.Info("command forwarded to agent",
		"agent_id", agent.ID,
		"function", req.FunctionName,
	)

	// For echo (only impl), agent handles display + broadcast; return success.
	// (Orchestrator result; display output visible to connected displays.)
	c.JSON(http.StatusOK, domain.CommandResultPayload{
		Success: true,
		Result:  fmt.Sprintf("command %q forwarded (see display for echo)", req.FunctionName),
	})
}
