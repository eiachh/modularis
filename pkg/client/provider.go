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
// Optionally holds a token for Authorization: Bearer header.
type Client struct {
	serverAddress string
	httpClient    *http.Client
	token         string // optional: Authorization: Bearer <token>
}

// New creates a new Client instance configured with the given server address.
// The serverAddress should be the base URL of the orchestrator (e.g., "http://localhost:8080").
func New(serverAddress string) *Client {
	return &Client{
		serverAddress: serverAddress,
		httpClient:    &http.Client{},
	}
}

// SetToken sets the bearer token for Authorization header on all requests.
func (c *Client) SetToken(token string) {
	c.token = token
}

// Token returns the current token (if any).
func (c *Client) Token() string {
	return c.token
}

// GetCapabilities fetches the list of all registered capabilities from the server.
// It returns a slice of Capability containing agent names, function names,
// and their respective JSON schemas.
// If a token was set via SetToken, it is sent as Authorization: Bearer.
func (c *Client) GetCapabilities() ([]Capability, error) {
	url := c.serverAddress + "/capabilities"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
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

// InvokeResponse is returned immediately by Invoke with the invocation ID.
type InvokeResponse struct {
	InvocationID string `json:"invocation_id"`
	Success      bool   `json:"success"`
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Invoke sends a command to the orchestrator and returns immediately with an
// invocation ID. Use GetInvokeResult(invocationID) to wait for and retrieve
// the result (blocking until the agent responds or acknowledges).
// If a token was set via SetToken, it is sent as Authorization: Bearer.
func (c *Client) Invoke(cmd InvokeCommand) (InvokeResponse, error) {
	url := c.serverAddress + "/invoke"

	payload, err := json.Marshal(cmd)
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("failed to marshal invoke command: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return InvokeResponse{}, ErrConnNotReady
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("failed to read invoke response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return InvokeResponse{}, fmt.Errorf("invoke failed with status %s: %s", resp.Status, string(body))
	}

	var ir InvokeResponse
	if err := json.Unmarshal(body, &ir); err != nil {
		return InvokeResponse{}, fmt.Errorf("failed to decode invoke response: %w", err)
	}
	return ir, nil
}

// InvokeResult is the result returned by GetInvokeResult.
type InvokeResult struct {
	CapabilityID string          `json:"capability_id"`
	Success      bool            `json:"success"`
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// GetInvokeResult blocks until the result for the given invocation ID is
// available from the orchestrator, then returns it. For fire-and-forget
// capabilities, the result may have success=true with empty result data
// (acknowledged only).
// If a token was set via SetToken, it is sent as Authorization: Bearer.
func (c *Client) GetInvokeResult(invocationID string) (InvokeResult, error) {
	url := fmt.Sprintf("%s/invoke/result/%s", c.serverAddress, invocationID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return InvokeResult{}, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return InvokeResult{}, ErrConnNotReady
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return InvokeResult{}, fmt.Errorf("failed to read result: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return InvokeResult{}, fmt.Errorf("invocation not found")
	}
	if resp.StatusCode != http.StatusOK {
		return InvokeResult{}, fmt.Errorf("result request failed with status %s: %s", resp.Status, string(body))
	}

	var ir InvokeResult
	if err := json.Unmarshal(body, &ir); err != nil {
		// Might be {"status":"acknowledged"} for fire-and-forget
		var ack struct {
			Status string `json:"status"`
		}
		if err2 := json.Unmarshal(body, &ack); err2 == nil && ack.Status == "acknowledged" {
			return InvokeResult{Success: true}, nil
		}
		return InvokeResult{}, fmt.Errorf("failed to decode result: %w", err)
	}
	return ir, nil
}