package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modularis/modularis/internal/domain"
	pkgdisplay "github.com/modularis/modularis/pkg/display"
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

	log.Info("connecting to orchestrator", "server", *server)

	d, err := pkgdisplay.Connect(*server)
	if err != nil {
		log.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer d.Close()

	events, err := d.Register(*name, *displayType)
	if err != nil {
		log.Error("registration failed", "error", err)
		os.Exit(1)
	}

	log.Info("registered successfully", "display_id", d.ID, "name", d.Name)
	log.Info("display running, listening for events (ctrl+c to stop)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case dp, ok := <-events:
			if !ok {
				log.Info("orchestrator closed the connection")
				return
			}
			render(log, dp)

		case <-sigCh:
			log.Info("shutting down")
			return
		}
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