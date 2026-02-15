package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"

	"github.com/modularis/modularis/internal/domain"
)

func main() {
	name := flag.String("name", "", "agent name (required)")
	server := flag.String("server", "ws://localhost:8080", "orchestrator base URL")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: -name is required")
		flag.Usage()
		os.Exit(1)
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	connectURL := *server + "/connect"
	log.Info("connecting to orchestrator", "url", connectURL)

	conn, _, err := websocket.DefaultDialer.Dial(connectURL, nil)
	if err != nil {
		log.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer conn.Close()

	log.Info("connected, sending register message")

	// --- Build and send register envelope ---------------------------------
	//
	// No capabilities in register payload (legacy support removed per
	// requirements). Only agent name is sent. Capabilities are registered
	// exclusively at runtime via capability_register message to support
	// dynamic component loading.

	regPayload, err := json.Marshal(domain.RegisterPayload{
		Name: *name,
	})
	if err != nil {
		log.Error("failed to marshal register payload", "error", err)
		os.Exit(1)
	}

	env := domain.Envelope{
		Type:    domain.MessageTypeRegister,
		Payload: regPayload,
	}

	if err := conn.WriteJSON(env); err != nil {
		log.Error("failed to send register message", "error", err)
		os.Exit(1)
	}

	// --- Wait for register_ack or error -----------------------------------

	var resp domain.Envelope
	if err := conn.ReadJSON(&resp); err != nil {
		log.Error("failed to read response", "error", err)
		os.Exit(1)
	}

	switch resp.Type {
	case domain.MessageTypeRegisterAck:
		var ack domain.RegisterAckPayload
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			log.Error("failed to decode register_ack", "error", err)
			os.Exit(1)
		}
		log.Info("registered successfully", "agent_id", ack.AgentID)

		// Notify all displays that this agent is up.
		dpPayload, _ := json.Marshal(domain.DisplayPayload{
			Title: "Agent Online",
			Body:  fmt.Sprintf("Agent: %s is up", ack.AgentID),
			Level: "info",
		})
		_ = conn.WriteJSON(domain.Envelope{
			Type:    domain.MessageTypeDisplay,
			Payload: dpPayload,
		})

		// --- Register the *only* capability: echo (runtime-only, no legacy) --
		//
		// After WS upgrade, dynamically register "echo" via capability_register.
		// This is the sole command; it echoes input to display. In real agents,
		// this would come from loaded components.
		capReg := domain.CapabilityRegisterPayload{
			AgentName:    *name,
			FunctionName: "echo",
			// JSON schema for required arg (message to echo).
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"message": {
						"type": "string",
						"description": "Message to echo back to display"
					}
				},
				"required": ["message"]
			}`),
		}
		capRegPayload, marshalErr := json.Marshal(capReg)
		if marshalErr != nil {
			log.Error("failed to marshal capability_register payload", "error", marshalErr)
		} else if err := conn.WriteJSON(domain.Envelope{
			Type:    domain.MessageTypeCapabilityRegister,
			Payload: capRegPayload,
		}); err != nil {
			log.Error("failed to send capability_register", "error", err)
		} else {
			log.Info("sent runtime capability registration",
				"function_name", capReg.FunctionName,
				"agent_name", capReg.AgentName,
			)
			// Orchestrator acks; logged in background read loop.
		}

		// Note: Command handling ("echo") is in the background read loop below
		// (receives forwarded commands from orchestrator).

	case domain.MessageTypeError:
		var errPayload domain.ErrorPayload
		if err := json.Unmarshal(resp.Payload, &errPayload); err != nil {
			log.Error("failed to decode error payload", "error", err)
			os.Exit(1)
		}
		log.Error("registration rejected", "code", errPayload.Code, "message", errPayload.Message)
		os.Exit(1)

	default:
		log.Error("unexpected response type", "type", resp.Type)
		os.Exit(1)
	}

	// --- Stay alive until interrupted -------------------------------------

	log.Info("agent running, waiting for messages (ctrl+c to stop)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Read loop in background so we notice if the orchestrator drops us.
	// Handles forwarded commands (e.g., "echo"): processes and sends to
	// display (as specified; only echo supported).
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg domain.Envelope
			if err := conn.ReadJSON(&msg); err != nil {
				log.Info("connection closed", "error", err)
				return
			}

			// Handle commands forwarded from orchestrator/client.
			switch msg.Type {
			case domain.MessageTypeCommand:
				var cmd domain.CommandPayload
				if err := json.Unmarshal(msg.Payload, &cmd); err != nil {
					log.Error("invalid command payload", "error", err)
					continue
				}
				log.Info("received command", "function", cmd.FunctionName, "agent", cmd.AgentName)

				// Only "echo" registered/supported: echo args to display.
				// (No legacy/other funcs.)
				if cmd.FunctionName == "echo" {
					var args struct {
						Message string `json:"message"`
					}
					if err := json.Unmarshal(cmd.Args, &args); err != nil {
						log.Error("invalid echo args", "error", err)
						continue
					}
					// Echo to display modules via existing payload.
					dpPayload, _ := json.Marshal(domain.DisplayPayload{
						Title: fmt.Sprintf("Echo from %s", cmd.AgentName),
						Body:  fmt.Sprintf("Echo: %s", args.Message),
						Level: "info",
					})
					if err := conn.WriteJSON(domain.Envelope{
						Type:    domain.MessageTypeDisplay,
						Payload: dpPayload,
					}); err != nil {
						log.Error("failed to broadcast echo to display", "error", err)
					} else {
						log.Info("echo sent to display", "message", args.Message)
					}
				} else {
					log.Warn("unknown command", "function", cmd.FunctionName)
				}

			default:
				log.Debug("received message", "type", msg.Type)
			}
		}
	}()

	select {
	case <-sigCh:
		log.Info("shutting down")
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "agent shutting down"),
		)
	case <-done:
		log.Info("orchestrator closed the connection")
	}
}