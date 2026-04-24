package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eiachh/Modularis/pkg/display"
)

func main() {
	name := flag.String("name", "", "display name (required)")
	displayType := flag.String("type", "terminal", "display type (terminal, web, led, discord, ...)")
	server := flag.String("server", "", "orchestrator base URL (falls back to ORCHESTRATOR_URL env or default)")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: -name is required")
		flag.Usage()
		os.Exit(1)
	}

	d := display.New(*server, *name, *displayType, 30*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Connecting display %q to orchestrator...\n", *name)
	id, err := d.Run(ctx, func(msg display.Message) {
		render(msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect display: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Display %s stopped.\n", id)
}

// render prints a display message to the terminal.
func render(msg display.Message) {
	fmt.Println("─────────────────────────────────────────")
	fmt.Printf("  Agent : %s (%s)\n", msg.AgentName, msg.AgentID)
	fmt.Printf("  Title : %s\n", msg.Title)
	fmt.Printf("  Level : %s\n", msg.Level)
	fmt.Println("─────────────────────────────────────────")
	fmt.Println(msg.Body)
	fmt.Println()
}
