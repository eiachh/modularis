package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrConnNotReady is returned when the orchestrator connection is not available.
var ErrConnNotReady = errors.New("orchestrator connection is not available")

// Capability represents a single capability returned by the server.
// It contains the agent name, function name, and the JSON schema for the
// capability's input arguments.
type Capability struct {
	// AgentName is the name of the agent providing this capability.
	AgentName string `json:"agent_name"`
	// FunctionName is the name of the capability/function.
	FunctionName string `json:"function_name"`
	// Schema is the JSON Schema describing the input arguments.
	Schema json.RawMessage `json:"schema"`
}

// InvokeCommand is the canonical "command send package" for invoking
// agent capabilities.
type InvokeCommand struct {
	// AgentName targets the agent (must match a registered agent name).
	AgentName string `json:"agent_name"`
	// FunctionName is the capability to invoke.
	FunctionName string `json:"function_name"`
	// Args is validated JSON input matching the capability's schema.
	Args json.RawMessage `json:"args"`
}

// Client provides functionality for interacting with a modularis orchestrator.
// It can fetch capabilities and assemble validated commands for invocation.
type Client struct {
	serverAddress string
	httpClient    *http.Client
}

// New creates a new Client instance configured with the given server address.
// The serverAddress should be the base URL of the orchestrator (e.g., "http://localhost:8080").
func New(serverAddress string) *Client {
	return &Client{
		serverAddress: serverAddress,
		httpClient:    &http.Client{},
	}
}

// GetCapabilities fetches the list of all registered capabilities from the server.
// It returns a slice of Capability containing agent names, function names,
// and their respective JSON schemas.
func (c *Client) GetCapabilities() ([]Capability, error) {
	url := c.serverAddress + "/capabilities"

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, ErrConnNotReady
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %s: %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var capabilities []Capability
	if err := json.Unmarshal(body, &capabilities); err != nil {
		return nil, fmt.Errorf("failed to decode capabilities: %w", err)
	}

	return capabilities, nil
}

// Invoke sends a command to the orchestrator for execution on the specified agent.
// The InvokeCommand should be constructed with valid agent name, function name, and args.
func (c *Client) Invoke(cmd InvokeCommand) error {
	url := c.serverAddress + "/invoke"

	payload, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal invoke command: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return ErrConnNotReady
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read invoke response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invoke failed with status %s: %s", resp.Status, string(body))
	}

	return nil
}