package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eiachh/Modularis/pkg/agent"
)

func main() {
	name := flag.String("name", "", "agent name (required)")
	server := flag.String("server", "", "orchestrator base URL (falls back to ORCHESTRATOR_URL or default)")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: -name is required")
		flag.Usage()
		os.Exit(1)
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	a := agent.New(*server, *name, 30*time.Second)

	schemas := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "Message to echo"
			}
		},
		"required": ["message"]
	}`)

	a.AddCapability("echoNoReturn", schemas)
	a.AddCapability("echoRespond", schemas)
	a.AddCapability("echoTimeout", schemas)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("connecting agent to orchestrator via provider")
	id, err := a.Run(ctx, func(inv agent.Invocation) {
		log.Info("received command", "function", inv.Name, "capability_id", inv.ID)

		var args struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(inv.Args, &args); err != nil {
			log.Error("invalid args", "error", err)
			return
		}

		switch inv.Name {
		case "echoNoReturn":
			a.SendDisplay(fmt.Sprintf("EchoNoReturn from %s", *name), fmt.Sprintf("Echo: %s", args.Message), "info")
			a.SendCommandResult(inv.ID, nil)
			log.Info("echoNoReturn acked")

		case "echoRespond":
			a.SendDisplay(fmt.Sprintf("EchoRespond from %s", *name), fmt.Sprintf("Echo: %s", args.Message), "info")
			result := map[string]string{"message": args.Message}
			resBytes, _ := json.Marshal(result)
			a.SendCommandResult(inv.ID, resBytes)
			log.Info("echoRespond result sent")

		case "echoTimeout":
			log.Info("echoTimeout received - intentionally not responding", "message", args.Message)

		default:
			log.Warn("unknown command", "function", inv.Name)
		}
	})
	if err != nil {
		log.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	log.Info("agent stopped", "agent_id", id)
}
