package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/eiachh/Modularis/pkg/client"
	"github.com/eiachh/Modularis/pkg/config"
)

// Policy Engine Demo
// ==================
// This client supports tokens for policy-protected calls.
//
// Usage with policy:
//   1. Get a token:    curl -X POST http://localhost:8080/token
//   2. Try invoke:     go run ./cmd/client -agent echo-agent -token <TOKEN>
//   3. SU grants:      (see examples/policy-demo.sh)
//   4. Retry invoke:   should succeed now
//
// Without -token, it auto-fetches a token from the orchestrator.

func main() {
	server := flag.String("server", "", "orchestrator address (default: from MODULARIS_SERVER or http://localhost:8080)")
	agent := flag.String("agent", "myagent", "agent name")
	token := flag.String("token", "", "bearer token for Authorization header (auto-fetched if not provided)")
	flag.Parse()

	serverURL := config.OrDefault(*server, config.GetServerURL())

	c := client.New(serverURL)

	// Auto-fetch token if not provided
	if *token == "" {
		autoToken, err := config.FetchToken(serverURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not auto-fetch token: %v\n", err)
			fmt.Fprintln(os.Stderr, "Running without token - some operations may fail.")
		} else {
			c.SetToken(autoToken)
			fmt.Printf("Auto-fetched token: %s...\n", autoToken[:min(20, len(autoToken))])
		}
	} else {
		c.SetToken(*token)
	}

	// Test cases
	tests := []struct {
		name     string
		function string
		args     map[string]any
		timeout  time.Duration
	}{
		{"echoNoReturn", "echoNoReturn", map[string]any{"message": "hello no-return"}, 5 * time.Second},
		{"echoRespond", "echoRespond", map[string]any{"message": "hello respond"}, 5 * time.Second},
		{"echoTimeout", "echoTimeout", map[string]any{"message": "hello timeout"}, 3 * time.Second},
	}

	for _, tc := range tests {
		fmt.Printf("\n=== %s ===\n", tc.name)

		cmd := client.InvokeCommand{
			AgentName:    *agent,
			FunctionName: tc.function,
			Args:         mustJSON(tc.args),
		}

		// Step 1: Invoke
		resp, err := c.Invoke(cmd)
		if err != nil {
			fmt.Printf("  Invoke error: %v\n", err)
			continue
		}
		fmt.Printf("  InvocationID: %s\n", resp.InvocationID)
		fmt.Printf("  Immediate result: %s\n", resp.Result)

		// Step 2: GetInvokeResult (blocking with timeout for demo)
		done := make(chan client.InvokeResult, 1)
		go func() {
			r, err := c.GetInvokeResult(resp.InvocationID)
			if err != nil {
				fmt.Printf("  GetInvokeResult error: %v\n", err)
				done <- client.InvokeResult{}
			} else {
				done <- r
			}
		}()

		select {
		case r := <-done:
			if r.CapabilityID != "" || r.Success {
				fmt.Printf("  Result: success=%v capability_id=%s result=%s\n",
					r.Success, r.CapabilityID, string(r.Result))
			}
		case <-time.After(tc.timeout):
			fmt.Printf("  GetInvokeResult timed out after %v (expected for echoTimeout)\n", tc.timeout)
		}
	}

	fmt.Println("\nDone.")
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		os.Exit(1)
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
