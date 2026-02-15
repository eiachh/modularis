package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	// pkg for the shared public InvokeCommand + Validate util (moved to pkg/
	// as requested for third-party availability per Go conventions; importable
	// as "github.com/modularis/modularis/pkg").
	"github.com/modularis/modularis/pkg"
	// domain for internal types (CapabilitySummary, CommandResultPayload).
	"github.com/modularis/modularis/internal/domain"
)

// Client is a dummy client for interacting with the orchestrator. It fetches
// and stores capabilities for validation, and can assemble command send
// packages for agent functions.
type Client struct {
	log          *slog.Logger
	serverURL    string
	// capabilities stores fetched caps (from domain.CapabilitySummary),
	// keyed by "agent_name:function_name" for fast lookup + validation before
	// assembling InvokeCommand (from pkg).
	capabilities map[string]domain.CapabilitySummary
}

// NewClient fetches and stores the capabilities list from /capabilities.
// The stored list is used for all subsequent command assembly/validation.
// Run after agents register runtime caps (no legacy support).
func NewClient(serverURL string, log *slog.Logger) (*Client, error) {
	url := serverURL + "/capabilities"
	log.Info("fetching capabilities", "url", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %s: %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	// Decode to domain.CapabilitySummary (internal domain type).
	var summaries []domain.CapabilitySummary
	if err := json.Unmarshal(body, &summaries); err != nil {
		return nil, fmt.Errorf("decode JSON failed: %w", err)
	}

	// Build lookup map for verification.
	capsMap := make(map[string]domain.CapabilitySummary)
	for _, s := range summaries {
		key := s.AgentName + ":" + s.FunctionName
		capsMap[key] = s
	}

	log.Info("capabilities loaded", "total", len(summaries))
	return &Client{
		log:          log,
		serverURL:    serverURL,
		capabilities: capsMap,
	}, nil
}

// AssembleAndValidateCommand assembles an InvokeCommand (the shared "command
// send package" from pkg) for the given agent + function with provided
// args (any JSON-marshalable value, e.g., map or struct).
//
// Validation (based on stored capabilities list, fetched once):
// 1. Agent has the requested function.
// 2. Args conform to the function's JSON schema (from registration).
//
// Returns the reusable package (ready to send via /invoke etc.) or error.
// This provides the requested verification for valid commands before
// transmission. Uses pkg.InvokeCommand (defined once in pkg/ for
// third-party reuse - no dups with internal structs).
func (c *Client) AssembleAndValidateCommand(agentName, functionName string, args any) (*pkg.InvokeCommand, error) {
	key := agentName + ":" + functionName
	cap, ok := c.capabilities[key]
	if !ok {
		return nil, fmt.Errorf("function %q not found on agent %q (check /capabilities)", functionName, agentName)
	}

	// Marshal args to JSON (required for RawMessage in InvokeCommand;
	// pkg.ValidateArgsAgainstSchema also marshals internally for validation -
	// dup is acceptable for separation of concerns/reuse).
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args failed: %w", err)
	}

	// Validate using shared util from pkg/ (moved as requested; accepts
	// schema directly for reuse by third parties/clients). Client extracts
	// schema from stored cap (domain.CapabilitySummary) and passes in.
	// (No local validator code - pkg handles.)
	if err := pkg.ValidateArgsAgainstSchema(argsJSON, cap.Schema); err != nil {
		// Wrap for context (e.g., which function failed).
		return nil, fmt.Errorf("invalid args for %q: %w", functionName, err)
	}

	c.log.Info("command assembled and validated",
		"agent", agentName,
		"function", functionName,
	)

	// Assemble shared package (from pkg; reusable by client,
	// orch handler, etc. - no dups with internal structs like old invokeReq).
	return &pkg.InvokeCommand{
		AgentName:    agentName,
		FunctionName: functionName,
		Args:         json.RawMessage(argsJSON),
	}, nil
}

// main creates a dummy client, fetches capabilities, and demonstrates
// AssembleAndValidateCommand (valid + invalid cases) for console output.
//
// Run with agent running (to populate runtime caps like "runtime-example").
func main() {
	server := flag.String("server", "http://localhost:8080", "orchestrator base URL")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	client, err := NewClient(*server, log)
	if err != nil {
		log.Error("client init failed", "error", err)
		os.Exit(1)
	}

	// Print discovered capabilities.
	fmt.Printf("Discovered capabilities (%d):\n", len(client.capabilities))
	for _, s := range client.capabilities {
		fmt.Printf("  - Agent: %s | Function: %s | Schema: %s\n",
			s.AgentName, s.FunctionName, string(s.Schema),
		)
	}

	// --- Demo: Assemble/validate + send command to orchestrator ----------
	// 1. AssembleAndValidateCommand (client-side, using stored caps list +
	//    JSON schema).
	// 2. If valid, send package to /invoke: orchestrator forwards to agent WS.
	//    Agent (echo only) processes + sends msg to display.
	// Uses stored caps list for verification (agent exists? schema valid?).

	// Valid echo command (only registered func; schema: {"message": "string"}).
	validArgs := map[string]string{"message": "hello from client"}
	pkg, err := client.AssembleAndValidateCommand("test-agent", "echo", validArgs)
	if err != nil {
		log.Error("valid command failed", "error", err)
	} else {
		fmt.Printf("Valid command package assembled: %+v\n", pkg)

		// Send to orchestrator /invoke (pkg matches expected payload).
		// Orch forwards to agent; echo result (via display) returned.
		invokeURL := client.serverURL + "/invoke"
		pkgJSON, _ := json.Marshal(pkg)
		resp, sendErr := http.Post(invokeURL, "application/json", bytes.NewReader(pkgJSON))
		if sendErr != nil {
			log.Error("send to /invoke failed", "error", sendErr)
		} else {
			defer resp.Body.Close()
			// Decode to domain.CommandResultPayload (internal type).
			var result domain.CommandResultPayload
			json.NewDecoder(resp.Body).Decode(&result)
			fmt.Printf("Command sent to orchestrator; result: %+v (echo visible in display)\n", result)
		}

		// --- Demo backend validation: direct POST with invalid args (bypasses
		// client Assemble/Validate to prove server-side schema check in /invoke
		// denies it using pkg.ValidateArgsAgainstSchema).
		// Args missing "message" (schema violation) - backend should return 400/
		// error in result. (Do not run app; edit-only per request.)
		invalidDirect := map[string]any{
			"agent_name":    "test-agent",
			"function_name": "echo",
			"args":          map[string]any{}, // violates required "message"
		}
		invalidJSON, _ := json.Marshal(invalidDirect)
		invResp, invErr := http.Post(invokeURL, "application/json", bytes.NewReader(invalidJSON))
		if invErr != nil {
			log.Error("direct invalid invoke failed", "error", invErr)
		} else {
			defer invResp.Body.Close()
			var invResult domain.CommandResultPayload
			json.NewDecoder(invResp.Body).Decode(&invResult)
			fmt.Printf("Backend denied invalid schema as expected: %+v\n", invResult)
		}
	}

	// Invalid: missing required "message" (schema violation) - caught client-side.
	invalidArgs := map[string]string{} // empty
	_, err = client.AssembleAndValidateCommand("test-agent", "echo", invalidArgs)
	if err != nil {
		fmt.Printf("Invalid command rejected as expected: %v\n", err)
	}

	// Invalid: unknown function.
	_, err = client.AssembleAndValidateCommand("test-agent", "nonexistent", validArgs)
	if err != nil {
		fmt.Printf("Unknown function rejected as expected: %v\n", err)
	}
}