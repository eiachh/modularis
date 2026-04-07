#!/usr/bin/env bash
#
# Policy Engine Demo Script
# =========================
#
# This script demonstrates the policy engine in the orchestrator.
# Prerequisites:
#   1. Start the orchestrator:  go run ./cmd/orchestrator
#   2. Start an agent:           go run ./cmd/agent -name echo-agent
#   3. Run this script in another terminal.
#
# What it shows:
#   - Default: client token has no permissions → invoke is forbidden
#   - SU grants a policy → client can invoke
#
# Usage:
#   ./examples/policy-demo.sh [ORCHESTRATOR_URL]
#   e.g., ./examples/policy-demo.sh http://localhost:8080

set -euo pipefail

BASE="${1:-http://localhost:8080}"

echo "=== Policy Engine Demo ==="
echo "Orchestrator: $BASE"
echo

# --- Step 1: Get a default client token (no permissions) ---
echo ">>> Step 1: Client requests a default token (no permissions)"
TOKEN=$(curl -s -X POST "$BASE/token" | jq -r '.token')
echo "Client token: ${TOKEN:0:40}..."
echo

# --- Step 2: Try to invoke without any policy (should fail) ---
echo ">>> Step 2: Client tries to invoke echoRespond (should be FORBIDDEN)"
HTTP_CODE=$(curl -s -o /tmp/invoke_resp.txt -w "%{http_code}" -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoRespond","args":{"message":"hello"}}')
echo "HTTP status: $HTTP_CODE"
echo "Response:"
cat /tmp/invoke_resp.txt | jq . 2>/dev/null || cat /tmp/invoke_resp.txt
echo

if [[ "$HTTP_CODE" != "403" && "$HTTP_CODE" != "401" ]]; then
  echo "WARNING: Expected 401/403, got $HTTP_CODE"
fi

# --- Step 3: SU obtains SU token ---
echo ">>> Step 3: SU obtains the SU token (one-time)"
SU_TOKEN=$(curl -s -X POST "$BASE/su/token" | jq -r '.token')
echo "SU token: ${SU_TOKEN:0:40}..."
echo

# --- Step 4: SU creates a role with echoRespond allowed ---
echo ">>> Step 4: SU creates a role 'basic_client' allowing echoRespond"
curl -s -X POST "$BASE/policy/role" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "basic_client",
    "rules": [
      {"service_id": "echo-agent", "capability": "echoRespond", "effect": "allow"}
    ]
  }' | jq .
echo

# --- Step 5: SU creates a policy for the client's token identity ---
echo ">>> Step 5: SU creates a policy for the client token, assigning basic_client role"
curl -s -X POST "$BASE/policy" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"identity\": \"$TOKEN\",
    \"roles\": [\"basic_client\"],
    \"rules\": []
  }" | jq .
echo

# --- Step 6: Client tries again (should succeed now) ---
echo ">>> Step 6: Client retries invoke echoRespond (should succeed)"
HTTP_CODE=$( /tmp/invoke_resp2.txt -w "%{http_code}" -X POST "$BASE/invoke" \
  -H "Authorization: Bearer curl -s -o$TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoRespond","args":{"message":"hello from policy demo"}}')
echo "HTTP status: $HTTP_CODE"
echo "Response:"
cat /tmp/invoke_resp2.txt | jq . 2>/dev/null || cat /tmp/invoke_resp2.txt
echo

if [[ "$HTTP_CODE" == "200" ]]; then
  echo "SUCCESS: Invoke allowed after policy grant!"
else
  echo "FAILED: Expected 200, got $HTTP_CODE"
fi

# --- Step 7: Show policy listing ---
echo ">>> Step 7: SU lists policies (showing our grant)"
curl -s -X GET "$BASE/policies" \
  -H "Authorization: Bearer $SU_TOKEN" | jq .
echo

echo "=== Demo Complete ==="
