package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/internal/ws"
)

// AgentService handles agent registration and runtime capability management.
type AgentService struct {
	Registry *registry.Registry
	Hubs     *ws.Hubs
	Log      *slog.Logger
}

// Register creates and persists a new agent, attaching it to the hub.
func (s *AgentService) Register(conn *ws.Conn, name string) (*domain.Agent, error) {
	agent := &domain.Agent{
		ID:           uuid.New().String(),
		Name:         name,
		Capabilities: nil,
		RegisteredAt: time.Now().UTC(),
	}

	if err := s.Registry.Add(agent); err != nil {
		s.Log.Error("registry add failed", "error", err)
		return nil, err
	}

	conn.Metadata = agent
	s.Hubs.Agent.Register(agent.ID, conn)

	s.Log.Info("agent registered",
		"agent_id", agent.ID,
		"name", agent.Name,
	)

	return agent, nil
}

// Deregister removes the agent from the hub and registry.
func (s *AgentService) Deregister(agentID string) {
	s.Hubs.Agent.Unregister(agentID)
	s.Registry.Remove(agentID)
	s.Log.Info("agent deregistered", "agent_id", agentID)
}

// RegisterCapability validates and stores a runtime capability for the agent.
func (s *AgentService) RegisterCapability(agent *domain.Agent, raw json.RawMessage) (domain.Capability, error) {
	var crp domain.CapabilityRegisterPayload
	if err := json.Unmarshal(raw, &crp); err != nil {
		s.Log.Error("invalid capability_register payload", "agent_id", agent.ID, "error", err)
		return domain.Capability{}, fmt.Errorf("INVALID_PAYLOAD: could not decode capability_register payload")
	}

	if crp.FunctionName == "" {
		return domain.Capability{}, fmt.Errorf("VALIDATION_ERROR: function_name is required")
	}
	if len(crp.Schema) == 0 {
		return domain.Capability{}, fmt.Errorf("VALIDATION_ERROR: schema is required")
	}

	if crp.AgentName != "" && crp.AgentName != agent.Name {
		s.Log.Warn("agent_name mismatch in capability registration",
			"agent_id", agent.ID,
			"expected", agent.Name,
			"got", crp.AgentName,
		)
	}

	cap := domain.Capability{
		Name:        crp.FunctionName,
		Description: fmt.Sprintf("Runtime-registered capability %q", crp.FunctionName),
		InputSchema: crp.Schema,
	}

	if err := s.Registry.RegisterCapability(agent.ID, cap); err != nil {
		s.Log.Error("capability registration failed",
			"agent_id", agent.ID,
			"capability", cap.Name,
			"error", err,
		)
		return domain.Capability{}, fmt.Errorf("REGISTRATION_FAILED: %w", err)
	}

	s.Log.Info("capability registered at runtime",
		"agent_id", agent.ID,
		"name", agent.Name,
		"capability", cap.Name,
	)

	return cap, nil
}

// BroadcastDisplay stamps the agent identity on a display payload and fans it out.
func (s *AgentService) BroadcastDisplay(agent *domain.Agent, raw json.RawMessage) {
	var dp domain.DisplayPayload
	if err := json.Unmarshal(raw, &dp); err != nil {
		s.Log.Warn("invalid display payload from agent", "agent_id", agent.ID, "error", err)
		return
	}

	dp.AgentID = agent.ID
	dp.AgentName = agent.Name

	s.Log.Info("broadcasting display message",
		"agent_id", agent.ID,
		"title", dp.Title,
		"displays", s.Hubs.Display.Count(),
	)

	s.Hubs.Display.Broadcast(domain.MessageTypeDisplay, dp)
}
