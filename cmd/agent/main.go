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

	capabilities := []domain.Capability{
		{
			Name:        "echo",
			Description: "Echoes back the input (placeholder capability)",
			Parameters: []domain.ParameterDef{
				{Name: "message", Type: "string", Required: true, Description: "The message to echo back"},
			},
		},
	}

	regPayload, err := json.Marshal(domain.RegisterPayload{
		Name:         *name,
		Capabilities: capabilities,
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg domain.Envelope
			if err := conn.ReadJSON(&msg); err != nil {
				log.Info("connection closed", "error", err)
				return
			}
			log.Debug("received message", "type", msg.Type)
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