package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eiachh/Modularis/pkg/agent"
	"github.com/eiachh/Modularis/pkg/client"
)

func main() {
	server := flag.String("server", "", "orchestrator base URL (ORCHESTRATOR_URL or default)")
	agentName := flag.String("agent", "hybrid-agent", "agent name for registration")
	flag.Parse()

	// 1. Setup Agent
	a := agent.New(*server, *agentName, 30*time.Second)

	msgSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "The message"
			}
		},
		"required": ["message"]
	}`)
	a.AddCapability("scream", msgSchema)
	a.AddCapability("longExec", msgSchema)
	a.AddCapability("fastExec", msgSchema)

	// 2. Run Agent in background (cancelled when interactive loop exits)
	agentCtx, agentCancel := context.WithCancel(context.Background())
	defer agentCancel()

	go func() {
		id, err := a.Run(agentCtx, func(inv agent.Invocation) {
			handleInvocation(inv)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to connect agent: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n[AGENT] stopped (ID: %s)\n", id)
	}()

	// Give the orchestrator a moment to process the registration
	time.Sleep(100 * time.Millisecond)

	// 3. Setup Client and run interactive loop
	c := client.New(*server)
	runInteractiveLoop(c)
}

func handleInvocation(inv agent.Invocation) {
	var args struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(inv.Args, &args); err != nil {
		fmt.Printf("\n[AGENT] Failed to unmarshal args for %s: %v\n", inv.Name, err)
		printPrompt()
		return
	}

	switch inv.Name {
	case "scream":
		fmt.Printf("\n[AGENT] SCREAMING: %s!!!\n", strings.ToUpper(args.Message))
	case "longExec":
		fmt.Printf("\n[AGENT] longExec started: %s (blocking for 30s)...\n", args.Message)
		time.Sleep(30 * time.Second)
		fmt.Printf("\n[AGENT] longExec completed: %s\n", args.Message)
	case "fastExec":
		fmt.Printf("\n[AGENT] fastExec started: %s (blocking for 5s)...\n", args.Message)
		time.Sleep(5 * time.Second)
		fmt.Printf("\n[AGENT] fastExec completed: %s\n", args.Message)
	default:
		fmt.Printf("\n[AGENT] Unknown function called: %s\n", inv.Name)
	}
	printPrompt()
}

func printPrompt() {
	fmt.Print("Enter capability (format: function_name message): ")
}

func runInteractiveLoop(c *client.Client) {
	for {
		capabilities, err := c.GetCapabilities()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get capabilities: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if len(capabilities) == 0 {
			fmt.Println("No capabilities available. Waiting...")
			time.Sleep(2 * time.Second)
			continue
		}

		printCapabilities(capabilities)

		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Enter capability (format: function_name message): ")

		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read input: %v\n", err)
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if line == "exit" || line == "quit" {
			break
		}

		processCommand(c, capabilities, line)
	}
}

func printCapabilities(capabilities []client.Capability) {
	fmt.Println("\nAvailable capabilities:")
	fmt.Println("----------------------")
	for i, cap := range capabilities {
		fmt.Printf("%d. %s:%s\n", i+1, cap.AgentName, cap.FunctionName)
	}
	fmt.Println("----------------------")
}

func processCommand(c *client.Client, capabilities []client.Capability, input string) {
	parts := strings.SplitN(input, " ", 2)
	if len(parts) < 2 {
		fmt.Println("Invalid format. Expected: function_name message")
		return
	}

	functionName := parts[0]
	message := parts[1]

	var targetCap *client.Capability
	for _, cap := range capabilities {
		if cap.FunctionName == functionName {
			targetCap = &cap
			break
		}
	}

	if targetCap == nil {
		fmt.Printf("Capability %q not found\n", functionName)
		return
	}

	args := map[string]string{"message": message}
	argsJSON, _ := json.Marshal(args)

	cmd := client.InvokeCommand{
		AgentName:    targetCap.AgentName,
		FunctionName: targetCap.FunctionName,
		Args:         json.RawMessage(argsJSON),
	}

	fmt.Printf("Invoking %s:%s...\n", targetCap.AgentName, targetCap.FunctionName)
	resp, err := c.Invoke(cmd)
	if err != nil {
		fmt.Printf("Invoke failed: %v\n", err)
	} else {
		fmt.Printf("Command sent! InvocationID: %s\n", resp.InvocationID)
	}
}
