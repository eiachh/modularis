package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/hub"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/pkg"
)

// Service orchestrates agent operations: registration, runtime capabilities,
// command invocation. Display broadcasts delegated to hub (no storage here;
// exactly as original pre-refactor).
// Application layer; handlers only decode/upgrade and delegate.
type Service struct {
	Registry   *registry.Registry
	AgentHub   *hub.Hub // for agent cmds/Get
	DisplayHub *hub.Hub // for broadcast only (no display storage)
	Log        *slog.Logger
}

// NewService wires agent service with hubs (displayHub for broadcast only).
func NewService(reg *registry.Registry, agentHub, displayHub *hub.Hub, log *slog.Logger) *Service {
	return &Service{
		Registry:   reg,
		AgentHub:   agentHub,
		DisplayHub: displayHub,
		Log:        log,
	}
}

// RegisterAgent validates name, creates agent via domain, persists to
// registry. Returns agent (for transport Metadata/conn) and ack payload.
// Hub registration is transport responsibility (prevents nil conns).
func (s *Service) RegisterAgent(name string) (*domain.Agent, domain.RegisterAckPayload, error) {
	if err := domain.ValidateName(name); err != nil {
		return nil, domain.RegisterAckPayload{}, err
	}

	agent := domain.NewAgent(name)

	if err := s.Registry.Add(agent); err != nil {
		return nil, domain.RegisterAckPayload{}, err
	}

	s.Log.Info("agent registered", "agent_id", agent.ID, "name", agent.Name, "initial_capabilities", len(agent.Capabilities))

	return agent, domain.RegisterAckPayload{AgentID: agent.ID}, nil
}

// ProcessDisplayMessage decodes/stamps display payload from agent then
// delegates to hub.Broadcast (exactly as original; no display storage).
func (s *Service) ProcessDisplayMessage(raw json.RawMessage, agent *domain.Agent) {
	var dp domain.DisplayPayload
	if err := json.Unmarshal(raw, &dp); err != nil {
		s.Log.Warn("invalid display payload", "agent_id", agent.ID, "error", err)
		return
	}

	dp.AgentID = agent.ID
	dp.AgentName = agent.Name

	s.Log.Info("broadcasting display", "agent_id", agent.ID, "title", dp.Title)
	s.DisplayHub.Broadcast(domain.MessageTypeDisplay, dp) // hub handles fanout
}

// RegisterCapability handles runtime reg for agent.
func (s *Service) RegisterCapability(raw json.RawMessage, agent *domain.Agent) error {
	var crp domain.CapabilityRegisterPayload
	if err := json.Unmarshal(raw, &crp); err != nil {
		return err
	}

	if crp.FunctionName == "" || len(crp.Schema) == 0 {
		return fmt.Errorf("invalid capability payload")
	}

	cap := domain.Capability{
		Name:        crp.FunctionName,
		Description: fmt.Sprintf("Runtime-registered %q", crp.FunctionName),
		InputSchema: crp.Schema,
	}

	if err := s.Registry.RegisterCapability(agent.ID, cap); err != nil {
		return err
	}

	s.Log.Info("capability registered", "agent_id", agent.ID, "name", agent.Name, "capability", cap.Name)
	return nil
}

// GetCapabilities returns flattened list for http handler.
func (s *Service) GetCapabilities() []domain.CapabilitySummary {
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
	s.Log.Info("served capabilities", "agents", len(agents), "total", len(summaries))
	return summaries
}

// InvokeCommand validates, forwards to agent WS via hub (core biz; display
// via broadcast in hub as original).
func (s *Service) InvokeCommand(req pkg.InvokeCommand) (domain.CommandResultPayload, error) {
	agent := s.Registry.GetByName(req.AgentName)
	if agent == nil {
		return domain.CommandResultPayload{Success: false, Error: fmt.Sprintf("agent %q not registered", req.AgentName)}, nil
	}

	var foundCap *domain.Capability
	for i := range agent.Capabilities {
		if agent.Capabilities[i].Name == req.FunctionName {
			cp := agent.Capabilities[i]
			foundCap = &cp
			break
		}
	}
	if foundCap == nil {
		return domain.CommandResultPayload{Success: false, Error: fmt.Sprintf("function %q not registered on agent", req.FunctionName)}, nil
	}

	if err := pkg.ValidateArgsAgainstSchema(req.Args, foundCap.InputSchema); err != nil {
		return domain.CommandResultPayload{Success: false, Error: fmt.Sprintf("invalid args: %v", err)}, nil
	}

	cmdPayload := domain.CommandPayload{
		AgentID:      agent.ID,
		AgentName:    req.AgentName,
		FunctionName: req.FunctionName,
		Args:         req.Args,
	}

	agentConn := s.AgentHub.Get(agent.ID)
	if agentConn == nil {
		return domain.CommandResultPayload{Success: false, Error: "agent WS lost"}, nil
	}

	if err := agentConn.SendEnvelope(domain.MessageTypeCommand, cmdPayload); err != nil {
		return domain.CommandResultPayload{Success: false, Error: err.Error()}, nil
	}

	s.Log.Info("command forwarded", "agent_id", agent.ID, "function", req.FunctionName)
	return domain.CommandResultPayload{Success: true, Result: fmt.Sprintf("command %q forwarded (see display)", req.FunctionName)}, nil
}

// CleanupAgent removes from hub/registry.
func (s *Service) CleanupAgent(agentID string) {
	s.AgentHub.Unregister(agentID)
	s.Registry.Remove(agentID)
	s.Log.Info("agent cleaned up", "agent_id", agentID)
}

// RegisterDisplay removed (display handled via global conn handler/hub; service
// agent-only as requested).
func (s *Service) RegisterDisplay(name, typ string) (*domain.Display, domain.DisplayRegisterAckPayload, error) {
	// Not used; display via conn handler.
	return nil, domain.DisplayRegisterAckPayload{}, fmt.Errorf("display handled via global conn handler")
}
