package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eiachh/Modularis/pkg/config"
	"github.com/eiachh/Modularis/pkg/display"
)

func main() {
	name := flag.String("name", "", "display name (required)")
	displayType := flag.String("type", "terminal", "display type (terminal, web, led, discord, ...)")
	server := flag.String("server", "", "orchestrator base URL (default: from MODULARIS_SERVER or http://localhost:8080)")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: -name is required")
		flag.Usage()
		os.Exit(1)
	}

	serverURL := config.OrDefault(*server, config.GetServerURL())

	d := display.New(serverURL, *name, *displayType, 30*time.Second)

	fmt.Printf("Connecting display %q to orchestrator...\n", *name)
	id, messages, closed, err := d.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect display: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Display connected and registered! (ID: %s)\n", id)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				fmt.Println("\n[DISPLAY] Message channel closed.")
				return
			}
			render(msg)
		case _, ok := <-closed:
			if !ok {
				return
			}
			fmt.Println("\n[DISPLAY] Connection to orchestrator lost! Waiting for reconnection...")
		case <-sigCh:
			fmt.Println("\nShutting down...")
			d.Close()
			return
		}
	}
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
