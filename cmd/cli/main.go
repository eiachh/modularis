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
	server := flag.String("server", "", "orchestrator base URL (ORCHESTRATOR_URL or default)")
	tokenFlag := flag.String("token", "", "use an existing SU token instead of claiming a new one")
	flag.Parse()

	// Resolve server address the same way the client provider does
	if *server == "" {
		if v := os.Getenv("ORCHESTRATOR_URL"); v != "" {
			*server = v
		} else {
			*server = "http://localhost:8080"
		}
	}

	fmt.Println("=== Modularis CLI ===")
	fmt.Printf("Server: %s\n\n", *server)

	var suToken string
	var isSU bool

	if *tokenFlag != "" {
		suToken = *tokenFlag
		isSU = true
		fmt.Println("Using provided SU token.")
		fmt.Println()
	} else {
		var err error
		suToken, err = claimSUToken(*server)
		if err != nil {
			fmt.Printf("Note: Could not claim SU token (%v)\n", err)
			fmt.Println("Running as regular client. Some operations will be unavailable.")
			fmt.Println("Tip: use -token <SU_TOKEN> to pass an existing SU token.")
			fmt.Println()
			isSU = false
		} else {
			fmt.Println("SU TOKEN (save this for admin use):")
			fmt.Println(suToken)
			fmt.Println()
			isSU = true
		}
	}

	// --- Build client ---
	c := client.New(*server)
	if isSU && suToken != "" {
		c.SetToken(suToken)
	}

	// --- Menu loop ---
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("----- MENU -----")
		fmt.Println("1) List capabilities (names only)")
		fmt.Println("2) Get capability schema (JSON)")
		fmt.Println("3) Invoke capability (agent + cap + json file)")
		if isSU {
			fmt.Println("4) Grant policy for all current capabilities (SU)")
			fmt.Println("5) List roles (SU)")
			fmt.Println("6) List policies (SU)")
			fmt.Println("7) Create role (SU)")
			fmt.Println("8) Create policy (SU)")
			fmt.Println("9) List tokens (SU)")
			fmt.Println("10) Create delegation grant (SU)")
			fmt.Println("11) List grants (SU)")
			fmt.Println("12) Revoke grant (SU)")
		}
		fmt.Println("13) Exit")
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
			if isSU {
				doGrantPolicyForAll(c, *server, suToken, scanner)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "5":
			if isSU {
				doListRoles(*server, suToken)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "6":
			if isSU {
				doListPolicies(*server, suToken)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "7":
			if isSU {
				doCreateRole(*server, suToken, scanner)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "8":
			if isSU {
				doCreatePolicy(c, *server, suToken, scanner)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "9":
			if isSU {
				doListTokens(c)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "10":
			if isSU {
				doCreateGrant(c, scanner)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "11":
			if isSU {
				doListGrants(c)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "12":
			if isSU {
				doRevokeGrant(c, scanner)
			} else {
				fmt.Println("This operation requires SU privileges.")
			}
		case "13":
			fmt.Println("Bye.")
			return
		default:
			fmt.Println("Unknown choice.")
		}
		fmt.Println()
	}
}

func claimSUToken(server string) (string, error) {
	if server == "" {
		if v := os.Getenv("ORCHESTRATOR_URL"); v != "" {
			server = v
		} else {
			server = "http://localhost:8080"
		}
	}
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

func grantPolicyForAll(c *client.Client, caps []client.Capability) error {
	// Create a role "su_cli_all" with allow for each (agent, cap)
	role := client.Role{Name: "su_cli_all"}
	for _, cap := range caps {
		role.Rules = append(role.Rules, client.RoleRule{
			ServiceID:  cap.AgentName,
			Capability: cap.FunctionName,
			Effect:     "allow",
		})
	}
	if err := c.CreateRole(role); err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	// Bind the SU token identity to that role
	pol := client.Policy{
		Identity: c.Token(),
		Roles:    []string{"su_cli_all"},
		Rules:    []client.RoleRule{},
	}
	if err := c.CreatePolicy(pol); err != nil {
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

	if err := grantPolicyForAll(c, caps); err != nil {
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

func doCreatePolicy(c *client.Client, server, token string, scanner *bufio.Scanner) {
	// Get current roles for reference
	body, _ := getJSON(server+"/policy/roles", token)
	var roleList struct {
		Roles []struct {
			Name string `json:"name"`
		} `json:"roles"`
	}
	_ = json.Unmarshal(body, &roleList) // ignore error, optional

	// Fetch tokens so the user can select by index
	tokensResp, _ := c.ListTokens()
	if len(tokensResp.Tokens) > 0 {
		fmt.Println("Available tokens:")
		for i, t := range tokensResp.Tokens {
			tokenPreview := t.Token
			if len(tokenPreview) > 40 {
				tokenPreview = tokenPreview[:40] + "..."
			}
			suMark := ""
			if t.IsSU {
				suMark = " [SU]"
			}
			fmt.Printf("  [%d] %s%s\n", i, tokenPreview, suMark)
		}
	}

	fmt.Print("Identity (token) [press Enter for SU token, or enter index]> ")
	if !scanner.Scan() {
		return
	}
	input := strings.TrimSpace(scanner.Text())
	var identity string
	if input == "" {
		identity = token
	} else if idx, err := strconv.Atoi(input); err == nil && idx >= 0 && idx < len(tokensResp.Tokens) {
		identity = tokensResp.Tokens[idx].Token
		preview := identity
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		fmt.Printf("Selected token: %s\n", preview)
	} else {
		identity = input
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

// --- Grant management functions ---

func doCreateGrant(c *client.Client, scanner *bufio.Scanner) {
	fmt.Println("=== Create Delegation Grant ===")
	fmt.Println("This allows a delegatee identity to act on behalf of a delegator for specific capabilities.")
	fmt.Println()

	// Fetch available tokens
	tokensResp, err := c.ListTokens()
	if err != nil {
		fmt.Printf("Warning: Could not fetch tokens: %v\n", err)
		fmt.Println("You will need to paste tokens manually.")
		fmt.Println()
	} else if len(tokensResp.Tokens) > 0 {
		fmt.Println("Available tokens:")
		for i, t := range tokensResp.Tokens {
			tokenPreview := t.Token
			if len(tokenPreview) > 40 {
				tokenPreview = tokenPreview[:40] + "..."
			}
			suMark := ""
			if t.IsSU {
				suMark = " [SU]"
			}
			fmt.Printf("  [%d] %s%s (created: %d)\n", i, tokenPreview, suMark, t.CreatedAt)
		}
		fmt.Println()
	}

	// Delegator
	fmt.Print("Delegator identity (who has permission) [press Enter for SU token")
	if len(tokensResp.Tokens) > 0 {
		fmt.Print(" or enter index")
	}
	fmt.Print("]> ")
	if !scanner.Scan() {
		return
	}
	input := strings.TrimSpace(scanner.Text())
	var delegator string

	if input == "" {
		// Default to SU token
		delegator = c.Token()
	} else if idx, err := strconv.Atoi(input); err == nil && idx >= 0 && idx < len(tokensResp.Tokens) {
		// User selected by index
		delegator = tokensResp.Tokens[idx].Token
		fmt.Printf("Selected token: %s...\n", delegator[:min(40, len(delegator))])
	} else {
		// User pasted a token
		delegator = input
	}

	// Delegatee
	fmt.Print("Delegatee identity (who will act on behalf of delegator)")
	if len(tokensResp.Tokens) > 0 {
		fmt.Print(" [enter index or paste token]")
	}
	fmt.Print("> ")
	if !scanner.Scan() {
		return
	}
	input = strings.TrimSpace(scanner.Text())
	var delegatee string

	if input == "" {
		fmt.Println("Delegatee is required.")
		return
	}

	if idx, err := strconv.Atoi(input); err == nil && idx >= 0 && idx < len(tokensResp.Tokens) {
		// User selected by index
		delegatee = tokensResp.Tokens[idx].Token
		fmt.Printf("Selected token: %s...\n", delegatee[:min(40, len(delegatee))])
	} else {
		delegatee = input
	}

	// Target agent
	fmt.Print("Target agent name (or '*' for any)> ")
	if !scanner.Scan() {
		return
	}
	targetAgent := strings.TrimSpace(scanner.Text())
	if targetAgent == "" {
		targetAgent = "*"
	}

	// Target capability
	fmt.Print("Target capability name (or '*' for any)> ")
	if !scanner.Scan() {
		return
	}
	targetCapability := strings.TrimSpace(scanner.Text())
	if targetCapability == "" {
		targetCapability = "*"
	}

	// Optional expiry
	var expiresAt int64
	fmt.Print("Expiry Unix timestamp (0 for no expiry) [0]> ")
	if !scanner.Scan() {
		return
	}
	expStr := strings.TrimSpace(scanner.Text())
	if expStr != "" {
		exp, err := strconv.ParseInt(expStr, 10, 64)
		if err != nil {
			fmt.Printf("Invalid timestamp: %v\n", err)
			return
		}
		expiresAt = exp
	}

	// Confirm
	fmt.Printf("\nGrant details:\n")
	fmt.Printf("  Delegator:        %s\n", delegator[:min(40, len(delegator))]+"...")
	fmt.Printf("  Delegatee:        %s\n", delegatee[:min(40, len(delegatee))]+"...")
	fmt.Printf("  Target Agent:     %s\n", targetAgent)
	fmt.Printf("  Target Capability: %s\n", targetCapability)
	if expiresAt > 0 {
		fmt.Printf("  Expires:          %d\n", expiresAt)
	} else {
		fmt.Println("  Expires:          never")
	}
	fmt.Print("\nCreate grant? (yes/no)> ")
	if !scanner.Scan() {
		return
	}
	if strings.TrimSpace(scanner.Text()) != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	// Create grant
	req := client.CreateGrantRequest{
		Delegator:        delegator,
		Delegatee:        delegatee,
		TargetAgent:      targetAgent,
		TargetCapability: targetCapability,
		ExpiresAt:        expiresAt,
	}

	resp, err := c.CreateGrant(req)
	if err != nil {
		fmt.Printf("Failed to create grant: %v\n", err)
		return
	}

	fmt.Printf("\nGrant created successfully!\n")
	fmt.Printf("  Created at: %d\n", resp.Grant.CreatedAt)
}

func doListGrants(c *client.Client) {
	resp, err := c.ListGrants()
	if err != nil {
		fmt.Printf("Error listing grants: %v\n", err)
		return
	}

	if len(resp.Grants) == 0 {
		fmt.Println("No grants found.")
		return
	}

	fmt.Printf("%d grant(s):\n", len(resp.Grants))
	for i, g := range resp.Grants {
		fmt.Printf("\n[%d] Grant:\n", i)
		fmt.Printf("  Delegator:         %s...\n", g.Delegator[:min(40, len(g.Delegator))])
		fmt.Printf("  Delegatee:         %s...\n", g.Delegatee[:min(40, len(g.Delegatee))])
		fmt.Printf("  Target Agent:      %s\n", g.TargetAgent)
		fmt.Printf("  Target Capability: %s\n", g.TargetCapability)
		if g.ExpiresAt > 0 {
			fmt.Printf("  Expires:           %d\n", g.ExpiresAt)
		}
		fmt.Printf("  Created:           %d\n", g.CreatedAt)
	}
}

func doRevokeGrant(c *client.Client, scanner *bufio.Scanner) {
	// First list grants for reference
	resp, err := c.ListGrants()
	if err != nil {
		fmt.Printf("Error listing grants: %v\n", err)
		return
	}

	if len(resp.Grants) == 0 {
		fmt.Println("No grants to revoke.")
		return
	}

	fmt.Println("Current grants:")
	for i, g := range resp.Grants {
		fmt.Printf("  [%d] %s -> %s (agent=%s, cap=%s)\n",
			i,
			g.Delegator[:min(20, len(g.Delegator))]+"...",
			g.Delegatee[:min(20, len(g.Delegatee))]+"...",
			g.TargetAgent,
			g.TargetCapability)
	}

	fmt.Print("\nSelect grant to revoke by index> ")
	if !scanner.Scan() {
		return
	}
	idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || idx < 0 || idx >= len(resp.Grants) {
		fmt.Println("Invalid index.")
		return
	}

	g := resp.Grants[idx]
	fmt.Printf("\nRevoking grant:\n")
	fmt.Printf("  Delegator:         %s...\n", g.Delegator[:min(40, len(g.Delegator))])
	fmt.Printf("  Delegatee:         %s...\n", g.Delegatee[:min(40, len(g.Delegatee))])
	fmt.Printf("  Target Agent:      %s\n", g.TargetAgent)
	fmt.Printf("  Target Capability: %s\n", g.TargetCapability)
	fmt.Print("\nConfirm revoke? (yes/no)> ")
	if !scanner.Scan() {
		return
	}
	if strings.TrimSpace(scanner.Text()) != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	req := client.RevokeGrantRequest{
		Delegator:        g.Delegator,
		Delegatee:        g.Delegatee,
		TargetAgent:      g.TargetAgent,
		TargetCapability: g.TargetCapability,
	}

	if err := c.RevokeGrant(req); err != nil {
		fmt.Printf("Failed to revoke grant: %v\n", err)
		return
	}

	fmt.Println("Grant revoked successfully!")
}

func doListTokens(c *client.Client) {
	resp, err := c.ListTokens()
	if err != nil {
		fmt.Printf("Error listing tokens: %v\n", err)
		return
	}

	if len(resp.Tokens) == 0 {
		fmt.Println("No tokens found.")
		return
	}

	fmt.Printf("%d token(s):\n", len(resp.Tokens))
	for i, t := range resp.Tokens {
		tokenPreview := t.Token
		if len(tokenPreview) > 50 {
			tokenPreview = tokenPreview[:50] + "..."
		}
		suMark := ""
		if t.IsSU {
			suMark = " [SU]"
		}
		createdAt := ""
		if t.CreatedAt > 0 {
			createdAt = fmt.Sprintf(" (created: %d)", t.CreatedAt)
		}
		fmt.Printf("  [%d] %s%s%s\n", i, tokenPreview, suMark, createdAt)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
