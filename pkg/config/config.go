package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const (
	// EnvHost is the environment variable for the orchestrator host.
	EnvHost = "MODULARIS_HOST"
	// EnvPort is the environment variable for the orchestrator port.
	EnvPort = "MODULARIS_PORT"
	// EnvServer is the environment variable for the full server URL (takes precedence over host/port).
	EnvServer = "MODULARIS_SERVER"

	// DefaultHost is the default host for the orchestrator.
	DefaultHost = "localhost"
	// DefaultPort is the default port for the orchestrator.
	DefaultPort = "8080"
)

// GetHost returns the orchestrator host from env var or default.
func GetHost() string {
	if v := os.Getenv(EnvHost); v != "" {
		return v
	}
	return DefaultHost
}

// GetPort returns the orchestrator port from env var or default.
func GetPort() string {
	if v := os.Getenv(EnvPort); v != "" {
		return v
	}
	return DefaultPort
}

// GetServerURL returns the full server URL (http://host:port).
// If MODULARIS_SERVER is set, it takes precedence.
func GetServerURL() string {
	if v := os.Getenv(EnvServer); v != "" {
		return v
	}
	return fmt.Sprintf("http://%s:%s", GetHost(), GetPort())
}

// GetWebSocketURL returns the WebSocket URL (ws://host:port).
// If MODULARIS_SERVER is set, it converts http(s) to ws(s).
func GetWebSocketURL() string {
	if v := os.Getenv(EnvServer); v != "" {
		// Convert http(s) to ws(s)
		if len(v) > 4 && v[:4] == "http" {
			return "ws" + v[4:]
		}
		return v
	}
	return fmt.Sprintf("ws://%s:%s", GetHost(), GetPort())
}

// GetListenAddr returns the listen address for the orchestrator (":port").
func GetListenAddr() string {
	return ":" + GetPort()
}

// tokenResponse is the response from POST /token or /su/token
type tokenResponse struct {
	Token string `json:"token"`
}

// FetchToken fetches a new token from the orchestrator.
func FetchToken(serverURL string) (string, error) {
	url := serverURL + "/token"
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status %s: %s", resp.Status, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if tr.Token == "" {
		return "", fmt.Errorf("empty token in response")
	}

	return tr.Token, nil
}

// FetchSUToken attempts to claim the SU token from the orchestrator.
// Returns the token if successful, or an error if already claimed or failed.
func FetchSUToken(serverURL string) (string, error) {
	url := serverURL + "/su/token"
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch SU token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusConflict {
		return "", fmt.Errorf("SU token already claimed")
	}

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status %s: %s", resp.Status, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if tr.Token == "" {
		return "", fmt.Errorf("empty token in response")
	}

	return tr.Token, nil
}

// OrDefault returns the value if non-empty, otherwise the fallback.
func OrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// PrettyJSON indents JSON bytes for display.
func PrettyJSON(b []byte) string {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, b, "", "  "); err != nil {
		return string(b)
	}
	return pretty.String()
}
