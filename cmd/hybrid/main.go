package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eiachh/Modularis/pkg/agent"
	"github.com/eiachh/Modularis/pkg/client"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "orchestrator base URL")
	agentName := flag.String("agent", "hybrid-agent", "agent name for registration")
	flag.Parse()

	// 1. Setup and Connect Agent
	invocations, closed := setupAgent(*server, *agentName)

	// 2. Handle Invocations in Background
	go handleEvents(invocations, closed)

	// Give the orchestrator a moment to process the registration
	time.Sleep(100 * time.Millisecond)

	// 3. Setup Client
	c := client.New(*server)

	// 4. Interactive Loop
	runInteractiveLoop(c)
}

func setupAgent(server, agentName string) (<-chan agent.Invocation, <-chan struct{}) {
	a := agent.New(server, agentName, 30*time.Second)

	// Add the "scream" capability
	screamSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "The message to scream"
			}
		},
		"required": ["message"]
	}`)
	a.AddCapability("scream", screamSchema)

	// Add the "longExec" capability - blocks for 30 seconds
	longExecSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "The message for long execution"
			}
		},
		"required": ["message"]
	}`)
	a.AddCapability("longExec", longExecSchema)

	// Add the "fastExec" capability - blocks for 5 seconds
	fastExecSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "The message for fast execution"
			}
		},
		"required": ["message"]
	}`)
	a.AddCapability("fastExec", fastExecSchema)

	fmt.Printf("Connecting agent %q to orchestrator...\n", agentName)
	id, invocations, closed, err := a.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect agent: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Agent connected and capabilities registered! (ID: %s)\n", id)
	return invocations, closed
}

func handleEvents(invocations <-chan agent.Invocation, closed <-chan struct{}) {
	var wg sync.WaitGroup

	for {
		select {
		case inv, ok := <-invocations:
			if !ok {
				return
			}
			wg.Add(1)
			go func(i agent.Invocation) {
				defer wg.Done()
				handleInvocation(i)
			}(inv)
		case _, ok := <-closed:
			if !ok {
				return
			}
			fmt.Println("\n[AGENT] Connection to orchestrator lost! Waiting for reconnection...")
			// In this hybrid client, we don't exit, we just log and wait.
			// The agent provider handles the actual reconnection.
		}
	}
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
