package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ErrConnNotReady is returned when the orchestrator connection is not available.
var ErrConnNotReady = errors.New("orchestrator connection is not available")

const defaultOrchestratorURL = "http://localhost:8080"

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
// If serverAddress is empty, it falls back to ORCHESTRATOR_URL env var or
// "http://localhost:8080". If no MODULARIS_TOKEN env var is set, a default
// token is automatically claimed from the orchestrator.
func New(serverAddress string) *Client {
	if serverAddress == "" {
		if v := os.Getenv("ORCHESTRATOR_URL"); v != "" {
			serverAddress = v
		} else {
			serverAddress = defaultOrchestratorURL
		}
	}
	c := &Client{
		serverAddress: serverAddress,
		httpClient:    &http.Client{},
	}
	if token := os.Getenv("MODULARIS_TOKEN"); token != "" {
		c.token = token
	} else {
		c.claimDefaultToken()
	}
	return c
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

// ErrResultTimeout is returned when GetInvokeResultCtx times out.
var ErrResultTimeout = errors.New("timed out waiting for invocation result")

// GetInvokeResultCtx polls for an invocation result, respecting the given
// context for cancellation/timeout. It retries every 500ms until a result
// is available, the context is cancelled, or a non-retryable error occurs.
func (c *Client) GetInvokeResultCtx(ctx context.Context, invocationID string) (InvokeResult, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		res, err := c.GetInvokeResult(invocationID)
		if err == nil {
			return res, nil
		}

		select {
		case <-ctx.Done():
			return InvokeResult{}, ErrResultTimeout
		case <-ticker.C:
		}
	}
}

// Grant represents a capability delegation grant.
type Grant struct {
	Delegator        string `json:"delegator"`
	Delegatee        string `json:"delegatee"`
	TargetAgent      string `json:"target_agent"`
	TargetCapability string `json:"target_capability"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
	CreatedAt        int64  `json:"created_at"`
}

// CreateGrantRequest is the request body for creating a grant.
type CreateGrantRequest struct {
	Delegator        string `json:"delegator"`
	Delegatee        string `json:"delegatee"`
	TargetAgent      string `json:"target_agent"`
	TargetCapability string `json:"target_capability"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
}

// CreateGrantResponse is the response from creating a grant.
type CreateGrantResponse struct {
	OK    bool  `json:"ok"`
	Grant Grant `json:"grant"`
}

// CreateGrant creates a delegation grant allowing a delegatee to act on behalf
// of a delegator for specific capabilities. Requires SU token.
func (c *Client) CreateGrant(req CreateGrantRequest) (CreateGrantResponse, error) {
	url := c.serverAddress + "/grant"

	payload, err := json.Marshal(req)
	if err != nil {
		return CreateGrantResponse{}, fmt.Errorf("failed to marshal grant request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return CreateGrantResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CreateGrantResponse{}, ErrConnNotReady
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CreateGrantResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return CreateGrantResponse{}, fmt.Errorf("grant creation failed with status %s: %s", resp.Status, string(body))
	}

	var gr CreateGrantResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return CreateGrantResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return gr, nil
}

// ListGrantsResponse is the response from listing grants.
type ListGrantsResponse struct {
	Grants []Grant `json:"grants"`
}

// ListGrants lists all grants in the system. Requires SU token.
func (c *Client) ListGrants() (ListGrantsResponse, error) {
	url := c.serverAddress + "/grants"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ListGrantsResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ListGrantsResponse{}, ErrConnNotReady
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ListGrantsResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ListGrantsResponse{}, fmt.Errorf("list grants failed with status %s: %s", resp.Status, string(body))
	}

	var lr ListGrantsResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return ListGrantsResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return lr, nil
}

// RevokeGrantRequest is the request body for revoking a grant.
type RevokeGrantRequest struct {
	Delegator        string `json:"delegator"`
	Delegatee        string `json:"delegatee"`
	TargetAgent      string `json:"target_agent"`
	TargetCapability string `json:"target_capability"`
}

// RevokeGrant revokes a specific grant. Requires SU token.
func (c *Client) RevokeGrant(req RevokeGrantRequest) error {
	url := c.serverAddress + "/grant"

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal revoke request: %w", err)
	}

	httpReq, err := http.NewRequest("DELETE", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ErrConnNotReady
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke grant failed with status %s: %s", resp.Status, string(body))
	}

	return nil
}

// TokenInfo represents information about a generated token.
type TokenInfo struct {
	Token     string `json:"token"`
	CreatedAt int64  `json:"created_at"`
	IsSU      bool   `json:"is_su"`
}

// ListTokensResponse is the response from listing tokens.
type ListTokensResponse struct {
	Tokens []TokenInfo `json:"tokens"`
}

// --- Policy types (now provided by the client for consistent server handling) ---

// RoleRule defines a single allow/deny rule for a service (agent) + capability.
type RoleRule struct {
	ServiceID  string `json:"service_id"`
	Capability string `json:"capability"`
	Effect     string `json:"effect"`
}

// Role represents a named collection of rules.
type Role struct {
	Name  string     `json:"name"`
	Rules []RoleRule `json:"rules"`
}

// Policy binds an identity (token) to roles and/or direct rules.
type Policy struct {
	Identity string     `json:"identity"`
	Roles    []string   `json:"roles"`
	Rules    []RoleRule `json:"rules"`
}

// CreateRole creates a new role (SU only).
func (c *Client) CreateRole(role Role) error {
	url := c.serverAddress + "/policy/role"

	payload, err := json.Marshal(role)
	if err != nil {
		return fmt.Errorf("failed to marshal role: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ErrConnNotReady
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create role failed: %s", string(body))
	}
	return nil
}

// ListRoles returns all roles (SU only).
func (c *Client) ListRoles() ([]Role, error) {
	url := c.serverAddress + "/policy/roles"

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list roles failed: %s", string(body))
	}

	var out struct {
		Roles []Role `json:"roles"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("failed to decode roles: %w", err)
	}
	return out.Roles, nil
}

// CreatePolicy creates or updates a policy for an identity (SU only).
func (c *Client) CreatePolicy(pol Policy) error {
	url := c.serverAddress + "/policy"

	payload, err := json.Marshal(pol)
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ErrConnNotReady
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create policy failed: %s", string(body))
	}
	return nil
}

// ListPolicies returns all policies (SU only).
func (c *Client) ListPolicies() ([]Policy, error) {
	url := c.serverAddress + "/policies"

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list policies failed: %s", string(body))
	}

	var out struct {
		Policies []Policy `json:"policies"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("failed to decode policies: %w", err)
	}
	return out.Policies, nil
}

// ListTokens lists all generated tokens. Requires SU token.
func (c *Client) ListTokens() (ListTokensResponse, error) {
	url := c.serverAddress + "/tokens"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ListTokensResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ListTokensResponse{}, ErrConnNotReady
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ListTokensResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ListTokensResponse{}, fmt.Errorf("list tokens failed with status %s: %s", resp.Status, string(body))
	}

	var lr ListTokensResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return ListTokensResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return lr, nil
}

// claimDefaultToken obtains a default token via POST /token and stores it.
// Called automatically by New when no MODULARIS_TOKEN is provided.
func (c *Client) claimDefaultToken() {
	url := c.serverAddress + "/token"
	resp, err := c.httpClient.Post(url, "application/json", nil)
	if err != nil || resp.StatusCode != http.StatusCreated {
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	defer resp.Body.Close()

	var tr struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err == nil && tr.Token != "" {
		c.token = tr.Token
	}
}
