package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/eiachh/Modularis/pkg/agent"
	"github.com/eiachh/Modularis/pkg/client"
)

// DeferredCallArgs represents the arguments for the deferredCall capability.
type DeferredCallArgs struct {
	TargetAgent      string          `json:"target_agent"`
	TargetCapability string          `json:"target_capability"`
	TargetArgs       json.RawMessage `json:"target_args"`
	DelaySeconds     int             `json:"delay_seconds"`
}

func main() {
	name := flag.String("name", "cron-service", "agent name")
	server := flag.String("server", "", "orchestrator base URL (falls back to ORCHESTRATOR_URL env or default)")
	token := flag.String("token", "", "bearer token (if empty, client auto-claims)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Client for re-invoking targets (auto claims token if needed)
	c := client.New(*server)
	if *token != "" {
		c.SetToken(*token)
	}

	a := agent.New(*server, *name, 30*time.Second)

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"target_agent": {"type": "string"},
			"target_capability": {"type": "string"},
			"target_args": {"type": "object"},
			"delay_seconds": {"type": "integer", "minimum": 1, "maximum": 3600}
		},
		"required": ["target_agent", "target_capability", "target_args", "delay_seconds"]
	}`)

	a.AddCapability("deferredCall", schema)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var pending sync.WaitGroup

	log.Info("connecting cron agent via provider")
	id, err := a.Run(ctx, func(inv agent.Invocation) {
		if inv.Name != "deferredCall" {
			return
		}

		var args DeferredCallArgs
		if err := json.Unmarshal(inv.Args, &args); err != nil {
			log.Error("invalid args", "error", err)
			a.SendCommandResult(inv.ID, mustJSON(map[string]any{"error": err.Error()}))
			return
		}

		log.Info("scheduling deferred", "target", args.TargetAgent+"/"+args.TargetCapability, "delay", args.DelaySeconds)

		// Immediate ack
		ack := map[string]string{"message": fmt.Sprintf("scheduled in %d seconds", args.DelaySeconds)}
		ackBytes, _ := json.Marshal(ack)
		a.SendCommandResult(inv.ID, ackBytes)

		pending.Add(1)
		go func(args DeferredCallArgs) {
			defer pending.Done()
			time.Sleep(time.Duration(args.DelaySeconds) * time.Second)

			log.Info("executing deferred call", "target", args.TargetAgent+"/"+args.TargetCapability)
			invokeCmd := client.InvokeCommand{
				AgentName:    args.TargetAgent,
				FunctionName: args.TargetCapability,
				Args:         args.TargetArgs,
			}
			resp, err := c.Invoke(invokeCmd)
			level := "success"
			body := resp.Result
			if err != nil {
				level = "error"
				body = err.Error()
			}
			a.SendDisplay(fmt.Sprintf("Deferred Call: %s/%s", args.TargetAgent, args.TargetCapability), body, level)
		}(args)
	})
	if err != nil {
		log.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	log.Info("cron stopped, waiting for pending calls", "agent_id", id)
	pending.Wait()
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
