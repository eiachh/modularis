package service

import (
	"fmt"
	"log/slog"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/internal/ws"
	"github.com/modularis/modularis/pkg"
)

// CapabilitiesService handles capability discovery and command invocation.
type CapabilitiesService struct {
	Registry *registry.Registry
	Hub      *ws.Hub
	Log      *slog.Logger
}

// ListSummaries returns a flat list of all capabilities across all agents.
func (s *CapabilitiesService) ListSummaries() []domain.CapabilitySummary {
	agents := s.Registry.List()

	var summaries []domain.CapabilitySummary
	for _, a := range agents {
		for _, cap := range a.Capabilities {
			summaries = append(summaries, domain.CapabilitySummary{
				AgentName:    a.Name,
				FunctionName: cap.Name,
				Schema:       cap.InputSchema,
			})
		}
	}

	s.Log.Info("served capabilities list",
		"agents", len(agents),
		"total_capabilities", len(summaries),
	)

	return summaries
}

// Invoke resolves the target agent, validates args against the capability
// schema, and forwards the command over the agent's WebSocket connection.
func (s *CapabilitiesService) Invoke(req pkg.InvokeCommand) (domain.CommandResultPayload, error) {
	agent := s.Registry.GetByName(req.AgentName)
	if agent == nil {
		return domain.CommandResultPayload{
			Success: false,
			Error:   fmt.Sprintf("agent %q not registered", req.AgentName),
		}, fmt.Errorf("agent not found")
	}

	cap := s.findCapability(agent, req.FunctionName)
	if cap == nil {
		return domain.CommandResultPayload{
			Success: false,
			Error:   fmt.Sprintf("function %q not registered on agent", req.FunctionName),
		}, fmt.Errorf("function not found")
	}

	s.Log.Debug("validating invoke args",
		"function", req.FunctionName,
		"args", string(req.Args),
		"schema_len", len(cap.InputSchema),
	)

	if err := pkg.ValidateArgsAgainstSchema(req.Args, cap.InputSchema); err != nil {
		s.Log.Warn("schema validation failed on invoke",
			"function", req.FunctionName,
			"agent", req.AgentName,
			"error", err,
		)
		return domain.CommandResultPayload{
			Success: false,
			Error:   fmt.Sprintf("invalid args for %q: %v", req.FunctionName, err),
		}, fmt.Errorf("schema validation failed")
	}

	agentConn := s.Hub.Get(agent.ID)
	if agentConn == nil {
		s.Log.Error("agent not connected in hub", "agent_id", agent.ID)
		return domain.CommandResultPayload{
			Success: false,
			Error:   "agent WS connection lost",
		}, fmt.Errorf("agent not connected")
	}

	cmdPayload := domain.CommandPayload{
		AgentID:      agent.ID,
		AgentName:    req.AgentName,
		FunctionName: req.FunctionName,
		Args:         req.Args,
	}

	if err := agentConn.SendEnvelope(domain.MessageTypeCommand, cmdPayload); err != nil {
		s.Log.Error("failed to forward command to agent", "error", err)
		return domain.CommandResultPayload{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	s.Log.Info("command forwarded to agent",
		"agent_id", agent.ID,
		"function", req.FunctionName,
	)

	return domain.CommandResultPayload{
		Success: true,
		Result:  fmt.Sprintf("command %q forwarded (see display for echo)", req.FunctionName),
	}, nil
}

// findCapability returns the named capability from the agent, or nil.
func (s *CapabilitiesService) findCapability(agent *domain.Agent, functionName string) *domain.Capability {
	for i := range agent.Capabilities {
		if agent.Capabilities[i].Name == functionName {
			cp := agent.Capabilities[i]
			return &cp
		}
	}
	return nil
}
