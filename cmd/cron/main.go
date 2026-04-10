package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"github.com/eiachh/Modularis/internal/domain"
	"github.com/eiachh/Modularis/pkg/client"
)

// DeferredCallArgs represents the arguments for the deferredCall capability.
type DeferredCallArgs struct {
	// TargetAgent is the agent to call
	TargetAgent string `json:"target_agent"`
	// TargetCapability is the capability to invoke
	TargetCapability string `json:"target_capability"`
	// TargetArgs are the arguments to pass to the target capability
	TargetArgs json.RawMessage `json:"target_args"`
	// DelaySeconds is how long to wait before calling
	DelaySeconds int `json:"delay_seconds"`
}

// CronService is a hybrid client+service that can schedule deferred capability calls.
type CronService struct {
	name     string
	conn     *websocket.Conn
	client   *client.Client
	log      *slog.Logger
	token    string
	pending  sync.WaitGroup
}

func main() {
	name := flag.String("name", "cron-service", "agent name")
	server := flag.String("server", "ws://localhost:8080", "orchestrator WebSocket URL")
	httpServer := flag.String("http-server", "http://localhost:8080", "orchestrator HTTP URL")
	token := flag.String("token", "", "bearer token for authorization (optional)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Create the cron service
	svc := &CronService{
		name:   *name,
		client: client.New(*httpServer),
		log:    log,
		token:  *token,
	}
	if *token != "" {
		svc.client.SetToken(*token)
	}

	// Connect as an agent
	connectURL := *server + "/connect"
	log.Info("connecting to orchestrator", "url", connectURL)

	conn, _, err := websocket.DefaultDialer.Dial(connectURL, nil)
	if err != nil {
		log.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer conn.Close()
	svc.conn = conn

	log.Info("connected, sending register message")

	// Register as an agent
	if err := svc.register(); err != nil {
		log.Error("registration failed", "error", err)
		os.Exit(1)
	}

	// Register capabilities
	if err := svc.registerCapabilities(); err != nil {
		log.Error("capability registration failed", "error", err)
		os.Exit(1)
	}

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Read loop
	done := make(chan struct{})
	go svc.readLoop(done)

	select {
	case <-sigCh:
		log.Info("shutting down")
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "cron service shutting down"),
		)
		// Wait for pending calls to complete
		svc.pending.Wait()
	case <-done:
		log.Info("orchestrator closed the connection")
	}
}

func (s *CronService) register() error {
	regPayload, err := json.Marshal(domain.RegisterPayload{
		Name: s.name,
	})
	if err != nil {
		return fmt.Errorf("marshal register payload: %w", err)
	}

	if err := s.conn.WriteJSON(domain.Envelope{
		Type:    domain.MessageTypeRegister,
		Payload: regPayload,
	}); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	var resp domain.Envelope
	if err := s.conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	switch resp.Type {
	case domain.MessageTypeRegisterAck:
		var ack domain.RegisterAckPayload
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			return fmt.Errorf("decode register_ack: %w", err)
		}
		s.log.Info("registered successfully", "agent_id", ack.AgentID)
		return nil
	case domain.MessageTypeError:
		var errPayload domain.ErrorPayload
		if err := json.Unmarshal(resp.Payload, &errPayload); err != nil {
			return fmt.Errorf("decode error: %w", err)
		}
		return fmt.Errorf("registration rejected: %s - %s", errPayload.Code, errPayload.Message)
	default:
		return fmt.Errorf("unexpected response type: %s", resp.Type)
	}
}

func (s *CronService) registerCapabilities() error {
	// Register the deferredCall capability
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"target_agent": {
				"type": "string",
				"description": "The agent to call after the delay"
			},
			"target_capability": {
				"type": "string",
				"description": "The capability to invoke on the target agent"
			},
			"target_args": {
				"type": "object",
				"description": "The arguments to pass to the target capability"
			},
			"delay_seconds": {
				"type": "integer",
				"description": "How many seconds to wait before making the call",
				"minimum": 1,
				"maximum": 3600
			}
		},
		"required": ["target_agent", "target_capability", "target_args", "delay_seconds"]
	}`)

	capReg := domain.CapabilityRegisterPayload{
		AgentName:    s.name,
		FunctionName: "deferredCall",
		Schema:       schema,
	}

	capRegPayload, err := json.Marshal(capReg)
	if err != nil {
		return fmt.Errorf("marshal capability_register: %w", err)
	}

	if err := s.conn.WriteJSON(domain.Envelope{
		Type:    domain.MessageTypeCapabilityRegister,
		Payload: capRegPayload,
	}); err != nil {
		return fmt.Errorf("send capability_register: %w", err)
	}

	s.log.Info("registered capability", "function_name", "deferredCall")
	return nil
}

func (s *CronService) readLoop(done chan struct{}) {
	defer close(done)

	for {
		var msg domain.Envelope
		if err := s.conn.ReadJSON(&msg); err != nil {
			s.log.Info("connection closed", "error", err)
			return
		}

		switch msg.Type {
		case domain.MessageTypeCommand:
			s.handleCommand(msg.Payload)
		default:
			s.log.Debug("received message", "type", msg.Type)
		}
	}
}

func (s *CronService) handleCommand(payload json.RawMessage) {
	var cmd domain.CommandPayload
	if err := json.Unmarshal(payload, &cmd); err != nil {
		s.log.Error("invalid command payload", "error", err)
		return
	}

	s.log.Info("received command",
		"function", cmd.FunctionName,
		"capability_id", cmd.CapabilityID,
	)

	switch cmd.FunctionName {
	case "deferredCall":
		s.handleDeferredCall(cmd)
	default:
		s.log.Warn("unknown command", "function", cmd.FunctionName)
	}
}

func (s *CronService) handleDeferredCall(cmd domain.CommandPayload) {
	var args DeferredCallArgs
	if err := json.Unmarshal(cmd.Args, &args); err != nil {
		s.log.Error("invalid deferredCall args", "error", err)
		s.sendError(cmd.CapabilityID, fmt.Sprintf("invalid args: %v", err))
		return
	}

	s.log.Info("scheduling deferred call",
		"target_agent", args.TargetAgent,
		"target_capability", args.TargetCapability,
		"delay_seconds", args.DelaySeconds,
	)

	// Send immediate acknowledgment
	s.sendAck(cmd.CapabilityID, fmt.Sprintf("scheduled to call %s/%s in %d seconds",
		args.TargetAgent, args.TargetCapability, args.DelaySeconds))

	// Schedule the call
	s.pending.Add(1)
	go func() {
		defer s.pending.Done()

		// Wait for the delay
		time.Sleep(time.Duration(args.DelaySeconds) * time.Second)

		// Make the call
		s.log.Info("executing deferred call",
			"target_agent", args.TargetAgent,
			"target_capability", args.TargetCapability,
		)

		invokeCmd := client.InvokeCommand{
			AgentName:    args.TargetAgent,
			FunctionName: args.TargetCapability,
			Args:         args.TargetArgs,
		}

		resp, err := s.client.Invoke(invokeCmd)
		if err != nil {
			s.log.Error("deferred call failed", "error", err)
			s.broadcastDisplay("Deferred Call Failed", fmt.Sprintf("Error: %v", err), "error")
			return
		}

		s.log.Info("deferred call completed",
			"invocation_id", resp.InvocationID,
			"success", resp.Success,
		)

		// Broadcast result to displays
		level := "success"
		if !resp.Success {
			level = "error"
		}
		s.broadcastDisplay(
			fmt.Sprintf("Deferred Call: %s/%s", args.TargetAgent, args.TargetCapability),
			fmt.Sprintf("Result: %s", resp.Result),
			level,
		)
	}()
}

func (s *CronService) sendAck(capabilityID, message string) {
	payload := map[string]any{
		"capability_id": capabilityID,
		"result":        map[string]string{"message": message},
	}
	payloadBytes, _ := json.Marshal(payload)

	if err := s.conn.WriteJSON(domain.Envelope{
		Type:    domain.MessageTypeCommandResult,
		Payload: payloadBytes,
	}); err != nil {
		s.log.Error("failed to send ack", "error", err)
	}
}

func (s *CronService) sendError(capabilityID, errMsg string) {
	payload := map[string]any{
		"capability_id": capabilityID,
		"success":       false,
		"error":         errMsg,
	}
	payloadBytes, _ := json.Marshal(payload)

	if err := s.conn.WriteJSON(domain.Envelope{
		Type:    domain.MessageTypeCommandResult,
		Payload: payloadBytes,
	}); err != nil {
		s.log.Error("failed to send error", "error", err)
	}
}

func (s *CronService) broadcastDisplay(title, body, level string) {
	dpPayload, _ := json.Marshal(domain.DisplayPayload{
		AgentID:   s.name,
		AgentName: s.name,
		Title:     title,
		Body:      body,
		Level:     level,
	})

	if err := s.conn.WriteJSON(domain.Envelope{
		Type:    domain.MessageTypeDisplay,
		Payload: dpPayload,
	}); err != nil {
		s.log.Error("failed to broadcast display", "error", err)
	}
}
