package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eiachh/Modularis/pkg/client"
)

// suTokenResponse is the response from POST /su/token
type suTokenResponse struct {
	Token string `json:"token"`
}

// policyResponse is a generic policy endpoint response
type policyResponse struct {
	OK bool `json:"ok"`
}

// policyRole is for creating a role
type policyRole struct {
	Name  string           `json:"name"`
	Rules []policyRoleRule `json:"rules"`
}

type policyRoleRule struct {
	ServiceID  string `json:"service_id"`
	Capability string `json:"capability"`
	Effect     string `json:"effect"`
}

// policyPolicy is for creating a policy binding identity->roles
type policyPolicy struct {
	Identity string   `json:"identity"`
	Roles    []string `json:"roles"`
	Rules    []any    `json:"rules"`
}

func main() {
	server := flag.String("server", "http://localhost:8080", "orchestrator base URL")
	flag.Parse()

	fmt.Println("=== Modularis SU CLI ===")
	fmt.Printf("Server: %s\n\n", *server)

	// --- Claim SU token ---
	suToken, err := claimSUToken(*server)
	if err != nil {
		fmt.Printf("ERROR: failed to claim SU token: %v\n", err)
		fmt.Println("Is the orchestrator running? (go run ./cmd/orchestrator)")
		os.Exit(1)
	}
	fmt.Println("SU TOKEN (save this for admin use):")
	fmt.Println(suToken)
	fmt.Println()

	// --- Build client with SU token ---
	c := client.New(*server)
	c.SetToken(suToken)

	// --- Menu loop ---
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("----- MENU -----")
		fmt.Println("1) List capabilities (names only)")
		fmt.Println("2) Get capability schema (JSON)")
		fmt.Println("3) Invoke capability (agent + cap + json file)")
		fmt.Println("4) Grant policy for all current capabilities (SU)")
		fmt.Println("5) List roles")
		fmt.Println("6) List policies")
		fmt.Println("7) Create role (interactive)")
		fmt.Println("8) Create policy (interactive)")
		fmt.Println("9) Exit")
		fmt.Print("> ")

		if !scanner.Scan() {
			break
		}
		choice := strings.TrimSpace(scanner.Text())

		switch choice {
		case "1":
			doListCapabilities(c)
		case "2":
			doGetSchema(c, scanner)
		case "3":
			doInvoke(c, *server, scanner)
		case "4":
			doGrantPolicyForAll(c, *server, suToken, scanner)
		case "5":
			doListRoles(*server, suToken)
		case "6":
			doListPolicies(*server, suToken)
		case "7":
			doCreateRole(*server, suToken, scanner)
		case "8":
			doCreatePolicy(*server, suToken, scanner)
		case "9":
			fmt.Println("Bye.")
			return
		default:
			fmt.Println("Unknown choice.")
		}
		fmt.Println()
	}
}

func claimSUToken(server string) (string, error) {
	url := server + "/su/token"
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		// Token already exists — try to re-fetch? Actually the API returns 409 with no token.
		// For simplicity, we just error. User can restart orchestrator or we could expose GET.
		return "", fmt.Errorf("SU token already claimed (409). Restart orchestrator to reset.")
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status %s: %s", resp.Status, string(body))
	}

	var tr suTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("bad response: %w", err)
	}
	if tr.Token == "" {
		return "", fmt.Errorf("empty token in response")
	}
	return tr.Token, nil
}

func grantPolicyForAll(server, suToken string, caps []client.Capability) error {
	// Create a role "su_cli_all" with allow for each (agent, cap)
	role := policyRole{Name: "su_cli_all"}
	for _, cap := range caps {
		role.Rules = append(role.Rules, policyRoleRule{
			ServiceID:  cap.AgentName,
			Capability: cap.FunctionName,
			Effect:     "allow",
		})
	}
	if err := postJSON(server+"/policy/role", suToken, role); err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	// Bind the SU token identity to that role
	pol := policyPolicy{
		Identity: suToken,
		Roles:    []string{"su_cli_all"},
		Rules:    []any{},
	}
	if err := postJSON(server+"/policy", suToken, pol); err != nil {
		return fmt.Errorf("create policy: %w", err)
	}
	return nil
}

func doGrantPolicyForAll(c *client.Client, server, suToken string, scanner *bufio.Scanner) {
	caps, err := c.GetCapabilities()
	if err != nil {
		fmt.Printf("Error fetching capabilities: %v\n", err)
		return
	}
	if len(caps) == 0 {
		fmt.Println("No capabilities registered. Nothing to grant.")
		return
	}

	fmt.Printf("This will grant invoke access for the SU token on ALL %d current capabilities:\n", len(caps))
	for _, cap := range caps {
		fmt.Printf("  - %s / %s\n", cap.AgentName, cap.FunctionName)
	}
	fmt.Print("\nType 'yes' to confirm: ")
	if !scanner.Scan() {
		return
	}
	confirm := strings.TrimSpace(scanner.Text())
	if confirm != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	if err := grantPolicyForAll(server, suToken, caps); err != nil {
		fmt.Printf("Failed: %v\n", err)
		return
	}
	fmt.Printf("Granted policy for %d capabilities.\n", len(caps))
}

// --- Role / Policy listing and interactive creation ---

func doListRoles(server, token string) {
	body, err := getJSON(server+"/policy/roles", token)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	// Pretty print
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(body))
	}
}

func doListPolicies(server, token string) {
	body, err := getJSON(server+"/policies", token)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(body))
	}
}

func doCreateRole(server, token string, scanner *bufio.Scanner) {
	fmt.Print("Role name> ")
	if !scanner.Scan() {
		return
	}
	name := strings.TrimSpace(scanner.Text())
	if name == "" {
		fmt.Println("Name required.")
		return
	}

	var rules []policyRoleRule
	for {
		fmt.Print("Add rule? (y/n)> ")
		if !scanner.Scan() {
			return
		}
		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			break
		}
		fmt.Print("  service_id (agent name)> ")
		if !scanner.Scan() {
			return
		}
		svc := strings.TrimSpace(scanner.Text())

		fmt.Print("  capability> ")
		if !scanner.Scan() {
			return
		}
		cap := strings.TrimSpace(scanner.Text())

		fmt.Print("  effect (allow/deny) [allow]> ")
		if !scanner.Scan() {
			return
		}
		eff := strings.TrimSpace(scanner.Text())
		if eff == "" {
			eff = "allow"
		}

		rules = append(rules, policyRoleRule{
			ServiceID:  svc,
			Capability: cap,
			Effect:     eff,
		})
	}

	role := policyRole{Name: name, Rules: rules}
	if err := postJSON(server+"/policy/role", token, role); err != nil {
		fmt.Printf("Failed: %v\n", err)
		return
	}
	fmt.Printf("Created role %q with %d rules.\n", name, len(rules))
}

func doCreatePolicy(server, token string, scanner *bufio.Scanner) {
	// Get current roles for reference
	body, _ := getJSON(server+"/policy/roles", token)
	var roleList struct {
		Roles []struct {
			Name string `json:"name"`
		} `json:"roles"`
	}
	_ = json.Unmarshal(body, &roleList) // ignore error, optional

	fmt.Printf("Identity (token) [press Enter for SU token]> ")
	if !scanner.Scan() {
		return
	}
	identity := strings.TrimSpace(scanner.Text())
	if identity == "" {
		identity = token
	}

	var roleNames []string
	if len(roleList.Roles) > 0 {
		fmt.Println("Available roles:")
		for i, r := range roleList.Roles {
			fmt.Printf("  [%d] %s\n", i, r.Name)
		}
		fmt.Print("Assign roles by index (e.g. 0,2) or 'none'> ")
		if !scanner.Scan() {
			return
		}
		pick := strings.TrimSpace(scanner.Text())
		if pick != "" && pick != "none" {
			for _, s := range strings.Split(pick, ",") {
				idx, err := strconv.Atoi(strings.TrimSpace(s))
				if err == nil && idx >= 0 && idx < len(roleList.Roles) {
					roleNames = append(roleNames, roleList.Roles[idx].Name)
				}
			}
		}
	} else {
		fmt.Print("No existing roles. Enter role names comma-separated or leave empty> ")
		if !scanner.Scan() {
			return
		}
		txt := strings.TrimSpace(scanner.Text())
		if txt != "" {
			for _, s := range strings.Split(txt, ",") {
				name := strings.TrimSpace(s)
				if name != "" {
					roleNames = append(roleNames, name)
				}
			}
		}
	}

	// Optional direct rules
	var rules []policyRule
	for {
		fmt.Print("Add direct rule? (y/n)> ")
		if !scanner.Scan() {
			return
		}
		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			break
		}
		fmt.Print("  service_id> ")
		if !scanner.Scan() {
			return
		}
		svc := strings.TrimSpace(scanner.Text())

		fmt.Print("  capability> ")
		if !scanner.Scan() {
			return
		}
		cap := strings.TrimSpace(scanner.Text())

		fmt.Print("  effect (allow/deny) [allow]> ")
		if !scanner.Scan() {
			return
		}
		eff := strings.TrimSpace(scanner.Text())
		if eff == "" {
			eff = "allow"
		}
		rules = append(rules, policyRule{
			ServiceID:  svc,
			Capability: cap,
			Effect:     eff,
		})
	}

	var rulesAny []any
	for _, r := range rules {
		rulesAny = append(rulesAny, r)
	}
	pol := policyPolicy{
		Identity: identity,
		Roles:    roleNames,
		Rules:    rulesAny,
	}
	if err := postJSON(server+"/policy", token, pol); err != nil {
		fmt.Printf("Failed: %v\n", err)
		return
	}
	fmt.Printf("Created policy for identity (len=%d) with %d roles, %d direct rules.\n",
		len(identity), len(roleNames), len(rules))
}

// policyRule is for direct rules in a policy (same shape as RoleRule)
type policyRule struct {
	ServiceID  string `json:"service_id"`
	Capability string `json:"capability"`
	Effect     string `json:"effect"`
}

func postJSON(url, token string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %s: %s", resp.Status, string(body))
	}
	return nil
}

// getJSON performs a GET with Bearer token and returns the body bytes.
func getJSON(url, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %s: %s", resp.Status, string(body))
	}
	return body, nil
}

func doListCapabilities(c *client.Client) {
	caps, err := c.GetCapabilities()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(caps) == 0 {
		fmt.Println("(no capabilities registered)")
		return
	}
	fmt.Printf("%d capabilities:\n", len(caps))
	for i, cap := range caps {
		fmt.Printf("  [%d] %s / %s\n", i, cap.AgentName, cap.FunctionName)
	}
}

func doGetSchema(c *client.Client, scanner *bufio.Scanner) {
	caps, err := c.GetCapabilities()
	if err != nil {
		fmt.Printf("Error fetching caps: %v\n", err)
		return
	}
	if len(caps) == 0 {
		fmt.Println("(no capabilities)")
		return
	}
	fmt.Println("Select capability by index:")
	for i, cap := range caps {
		fmt.Printf("  [%d] %s / %s\n", i, cap.AgentName, cap.FunctionName)
	}
	fmt.Print("index> ")
	if !scanner.Scan() {
		return
	}
	idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || idx < 0 || idx >= len(caps) {
		fmt.Println("Invalid index.")
		return
	}
	schema := caps[idx].Schema
	// Pretty print
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, schema, "", "  "); err == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(schema))
	}
}

func doInvoke(c *client.Client, server string, scanner *bufio.Scanner) {
	fmt.Print("Agent name> ")
	if !scanner.Scan() {
		return
	}
	agent := strings.TrimSpace(scanner.Text())

	fmt.Print("Capability name> ")
	if !scanner.Scan() {
		return
	}
	capName := strings.TrimSpace(scanner.Text())

	fmt.Print("Path to JSON args file> ")
	if !scanner.Scan() {
		return
	}
	path := strings.TrimSpace(scanner.Text())

	argsBytes, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Cannot read file: %v\n", err)
		return
	}
	// Validate it's JSON
	var js json.RawMessage
	if err := json.Unmarshal(argsBytes, &js); err != nil {
		fmt.Printf("File is not valid JSON: %v\n", err)
		return
	}

	cmd := client.InvokeCommand{
		AgentName:    agent,
		FunctionName: capName,
		Args:         js,
	}

	// Invoke
	invResp, err := c.Invoke(cmd)
	if err != nil {
		fmt.Printf("Invoke error: %v\n", err)
		return
	}
	fmt.Printf("InvocationID: %s\n", invResp.InvocationID)
	fmt.Printf("Immediate: success=%v result=%s error=%s\n",
		invResp.Success, invResp.Result, invResp.Error)

	// Poll for result (up to 10s)
	fmt.Println("Waiting for result...")
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		res, err := c.GetInvokeResult(invResp.InvocationID)
		if err != nil {
			// Not ready yet
			continue
		}
		fmt.Printf("Result: success=%v\n", res.Success)
		if len(res.Result) > 0 {
			var pretty bytes.Buffer
			if json.Indent(&pretty, res.Result, "", "  ") == nil {
				fmt.Println(pretty.String())
			} else {
				fmt.Println(string(res.Result))
			}
		}
		if res.Error != "" {
			fmt.Printf("Error: %s\n", res.Error)
		}
		return
	}
	fmt.Println("(timed out waiting for result)")
}
