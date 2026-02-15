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
	name := flag.String("name", "", "display name (required)")
	displayType := flag.String("type", "terminal", "display type (terminal, web, led, discord, ...)")
	server := flag.String("server", "ws://localhost:8080", "orchestrator base URL")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: -name is required")
		flag.Usage()
		os.Exit(1)
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	connectURL := *server + "/display"
	log.Info("connecting to orchestrator", "url", connectURL)

	conn, _, err := websocket.DefaultDialer.Dial(connectURL, nil)
	if err != nil {
		log.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer conn.Close()

	log.Info("connected, sending display_register message")

	// --- Build and send display_register envelope -------------------------

	regPayload, err := json.Marshal(domain.DisplayRegisterPayload{
		Name: *name,
		Type: *displayType,
	})
	if err != nil {
		log.Error("failed to marshal display_register payload", "error", err)
		os.Exit(1)
	}

	env := domain.Envelope{
		Type:    domain.MessageTypeDisplayRegister,
		Payload: regPayload,
	}

	if err := conn.WriteJSON(env); err != nil {
		log.Error("failed to send display_register message", "error", err)
		os.Exit(1)
	}

	// --- Wait for display_register_ack or error ---------------------------

	var resp domain.Envelope
	if err := conn.ReadJSON(&resp); err != nil {
		log.Error("failed to read response", "error", err)
		os.Exit(1)
	}

	switch resp.Type {
	case domain.MessageTypeDisplayRegisterAck:
		var ack domain.DisplayRegisterAckPayload
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			log.Error("failed to decode display_register_ack", "error", err)
			os.Exit(1)
		}
		log.Info("registered successfully", "display_id", ack.DisplayID)

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

	// --- Listen for display messages --------------------------------------

	log.Info("display running, listening for events (ctrl+c to stop)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg domain.Envelope
			if err := conn.ReadJSON(&msg); err != nil {
				log.Info("connection closed", "error", err)
				return
			}

			switch msg.Type {
			case domain.MessageTypeDisplay:
				var dp domain.DisplayPayload
				if err := json.Unmarshal(msg.Payload, &dp); err != nil {
					log.Warn("failed to decode display payload", "error", err)
					continue
				}
				render(log, dp)

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
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "display shutting down"),
		)
	case <-done:
		log.Info("orchestrator closed the connection")
	}
}

// render prints a display payload to the terminal.
func render(log *slog.Logger, dp domain.DisplayPayload) {
	fmt.Println("─────────────────────────────────────────")
	fmt.Printf("  Agent : %s (%s)\n", dp.AgentName, dp.AgentID)
	fmt.Printf("  Title : %s\n", dp.Title)
	fmt.Printf("  Level : %s\n", dp.Level)
	fmt.Println("─────────────────────────────────────────")
	fmt.Println(dp.Body)
	fmt.Println()
}