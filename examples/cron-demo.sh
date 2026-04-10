#!/usr/bin/env bash
#
# Cron Service Delegation Demo
# ============================
#
# This script demonstrates the cron hybrid service with capability delegation.
# Prerequisites:
#   1. Start the orchestrator:  go run ./cmd/orchestrator
#   2. Start an agent:           go run ./cmd/agent -name echo-agent -server ws://localhost:8080
#   3. Start the cron service:   go run ./cmd/cron -token <CRON_TOKEN>
#   4. Run this script in another terminal.
#
# What it shows:
#   - SU lists available tokens
#   - SU creates a grant for the cron service
#   - Client asks cron service to call a capability after a delay
#   - Cron service uses the grant to invoke on behalf of SU
#
# Usage:
#   ./examples/cron-demo.sh [ORCHESTRATOR_URL]
#   e.g., ./examples/cron-demo.sh http://localhost:8080

set -euo pipefail

BASE="${1:-http://localhost:8080}"

echo "=== Cron Service Delegation Demo ==="
echo "Orchestrator: $BASE"
echo

# --- Step 1: SU obtains SU token ---
echo ">>> Step 1: SU obtains the SU token"
SU_TOKEN=$(curl -s -X POST "$BASE/su/token" | jq -r '.token')
echo "SU token: ${SU_TOKEN:0:40}..."
echo

# --- Step 2: Get a token for the cron service ---
echo ">>> Step 2: Get a token for the cron service"
CRON_TOKEN=$(curl -s -X POST "$BASE/token" | jq -r '.token')
echo "Cron service token: ${CRON_TOKEN:0:40}..."
echo

# --- Step 3: List available tokens ---
echo ">>> Step 3: List all available tokens (new endpoint!)"
curl -s -X GET "$BASE/tokens" \
  -H "Authorization: Bearer $SU_TOKEN" | jq .
echo

# --- Step 4: SU creates a role with echoRespond allowed ---
echo ">>> Step 4: SU creates a role 'echo_user' allowing echoRespond"
curl -s -X POST "$BASE/policy/role" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "echo_user",
    "rules": [
      {"service_id": "echo-agent", "capability": "echoRespond", "effect": "allow"}
    ]
  }' | jq .
echo

# --- Step 5: SU creates a policy for itself ---
echo ">>> Step 5: SU creates a policy for itself, assigning echo_user role"
curl -s -X POST "$BASE/policy" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"identity\": \"$SU_TOKEN\",
    \"roles\": [\"echo_user\"],
    \"rules\": []
  }' | jq .
echo

# --- Step 6: SU creates a delegation grant for the cron service ---
echo ">>> Step 6: SU creates a delegation grant for the cron service"
echo "    This allows cron to call echo-agent/echoRespond on SU's behalf"
curl -s -X POST "$BASE/grant" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"delegator\": \"$SU_TOKEN\",
    \"delegatee\": \"$CRON_TOKEN\",
    \"target_agent\": \"echo-agent\",
    \"target_capability\": \"echoRespond\"
  }" | jq .
echo

# --- Step 7: List grants ---
echo ">>> Step 7: List all grants"
curl -s -X GET "$BASE/grants" \
  -H "Authorization: Bearer $SU_TOKEN" | jq .
echo

# --- Step 8: Start the cron service with its token ---
echo ">>> Step 8: Start the cron service"
echo "    Run this in another terminal:"
echo "    go run ./cmd/cron -token $CRON_TOKEN"
echo
echo "    The cron service will:"
echo "    - Connect as an agent named 'cron-service'"
echo "    - Expose a 'deferredCall' capability"
echo "    - Use its token to invoke other capabilities (via the grant)"
echo
read -p "Press Enter when the cron service is running..."
echo

# --- Step 9: List capabilities (should show cron-service) ---
echo ">>> Step 9: List capabilities (should show cron-service)"
curl -s -X GET "$BASE/capabilities" | jq .
echo

# --- Step 10: Client asks cron to schedule a deferred call ---
echo ">>> Step 10: Client asks cron service to call echo-agent after 3 seconds"
echo "    Note: The cron service will use the GRANT to invoke on SU's behalf"
HTTP_CODE=$(curl -s -o /tmp/cron_resp.txt -w "%{http_code}" -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_name": "cron-service",
    "function_name": "deferredCall",
    "args": {
      "target_agent": "echo-agent",
      "target_capability": "echoRespond",
      "target_args": {"message": "Hello from deferred call!"},
      "delay_seconds": 3
    }
  }')
echo "HTTP status: $HTTP_CODE"
echo "Response:"
cat /tmp/cron_resp.txt | jq . 2>/dev/null || cat /tmp/cron_resp.txt
echo

if [[ "$HTTP_CODE" == "200" ]]; then
  echo "SUCCESS: Cron service accepted the deferred call request!"
  echo "Watch the orchestrator logs for the deferred call execution in 3 seconds..."
else
  echo "FAILED: Expected 200, got $HTTP_CODE"
fi

echo
echo "=== Demo Complete ==="
echo
echo "Summary of the flow:"
echo "  1. SU has permission to call echo-agent/echoRespond"
echo "  2. SU created a grant: SU -> cron-service for echo-agent/echoRespond"
echo "  3. Client asked cron-service to call echo-agent after 3 seconds"
echo "  4. Cron-service (with no direct permissions) used the grant"
echo "  5. Cron-service invoked echo-agent on SU's behalf"
