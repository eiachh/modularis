#!/usr/bin/env bash
#
# Grant Delegation Demo Script
# ============================
#
# This script demonstrates capability token delegation via grants.
# Prerequisites:
#   1. Start the orchestrator:  go run ./cmd/orchestrator
#   2. Start an agent:           go run ./cmd/agent -name echo-agent -server ws://localhost:8080
#   3. Run this script in another terminal.
#
# What it shows:
#   - SU client creates a grant allowing a delegatee to act on SU's behalf
#   - Delegatee (with no direct permissions) can invoke via the grant
#
# Usage:
#   ./examples/grant-demo.sh [ORCHESTRATOR_URL]
#   e.g., ./examples/grant-demo.sh http://localhost:8080

set -euo pipefail

BASE="${1:-http://localhost:8080}"

echo "=== Grant Delegation Demo ==="
echo "Orchestrator: $BASE"
echo

# --- Step 1: SU obtains SU token ---
echo ">>> Step 1: SU obtains the SU token (one-time)"
SU_TOKEN=$(curl -s -X POST "$BASE/su/token" | jq -r '.token')
echo "SU token: ${SU_TOKEN:0:40}..."
echo

# --- Step 2: Get a token for the delegatee (cronjob service) ---
echo ">>> Step 2: Get a token for the delegatee (cronjob-service)"
DELEGATEE_TOKEN=$(curl -s -X POST "$BASE/token" | jq -r '.token')
echo "Delegatee token: ${DELEGATEE_TOKEN:0:40}..."
echo

# --- Step 3: Delegatee tries to invoke (should fail - no permissions) ---
echo ">>> Step 3: Delegatee tries to invoke echoRespond (should be FORBIDDEN)"
HTTP_CODE=$(curl -s -o /tmp/invoke_fail.txt -w "%{http_code}" -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $DELEGATEE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoRespond","args":{"message":"hello from delegatee"}}')
echo "HTTP status: $HTTP_CODE"
echo "Response:"
cat /tmp/invoke_fail.txt | jq . 2>/dev/null || cat /tmp/invoke_fail.txt
echo

if [[ "$HTTP_CODE" != "403" && "$HTTP_CODE" != "401" ]]; then
  echo "WARNING: Expected 401/403, got $HTTP_CODE"
fi

# --- Step 4: SU creates a role with echoRespond allowed ---
echo ">>> Step 4: SU creates a role 'capability_a_user' allowing echoRespond"
curl -s -X POST "$BASE/policy/role" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "capability_a_user",
    "rules": [
      {"service_id": "echo-agent", "capability": "echoRespond", "effect": "allow"}
    ]
  }' | jq .
echo

# --- Step 5: SU creates a policy for itself (the delegator) ---
echo ">>> Step 5: SU creates a policy for itself, assigning capability_a_user role"
curl -s -X POST "$BASE/policy" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"identity\": \"$SU_TOKEN\",
    \"roles\": [\"capability_a_user\"],
    \"rules\": []
  }' | jq .
echo

# --- Step 6: SU creates a DELEGATION GRANT ---
echo ">>> Step 6: SU creates a delegation grant for the delegatee"
echo "    This allows delegatee to act on SU's behalf for echo-agent/echoRespond"
GRANT_RESP=$(curl -s -X POST "$BASE/grant" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"delegator\": \"$SU_TOKEN\",
    \"delegatee\": \"$DELEGATEE_TOKEN\",
    \"target_agent\": \"echo-agent\",
    \"target_capability\": \"echoRespond\"
  }")
echo "$GRANT_RESP" | jq .
echo

# --- Step 7: List grants ---
echo ">>> Step 7: List all grants"
curl -s -X GET "$BASE/grants" \
  -H "Authorization: Bearer $SU_TOKEN" | jq .
echo

# --- Step 8: Delegatee tries again (should succeed via grant!) ---
echo ">>> Step 8: Delegatee retries invoke echoRespond (should succeed via grant!)"
HTTP_CODE=$(curl -s -o /tmp/invoke_success.txt -w "%{http_code}" -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $DELEGATEE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoRespond","args":{"message":"hello via delegation!"}}')
echo "HTTP status: $HTTP_CODE"
echo "Response:"
cat /tmp/invoke_success.txt | jq . 2>/dev/null || cat /tmp/invoke_success.txt
echo

if [[ "$HTTP_CODE" == "200" ]]; then
  echo "SUCCESS: Delegatee invoked capability via grant delegation!"
else
  echo "FAILED: Expected 200, got $HTTP_CODE"
fi

# --- Step 9: Show what happens when delegatee tries a different capability ---
echo ">>> Step 9: Delegatee tries a different capability (should fail - not in grant)"
HTTP_CODE=$(curl -s -o /tmp/invoke_other.txt -w "%{http_code}" -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $DELEGATEE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoNoReturn","args":{"message":"hello"}}')
echo "HTTP status: $HTTP_CODE"
echo "Response:"
cat /tmp/invoke_other.txt | jq . 2>/dev/null || cat /tmp/invoke_other.txt
echo

# --- Step 10: Revoke the grant ---
echo ">>> Step 10: SU revokes the grant"
curl -s -X DELETE "$BASE/grant" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"delegator\": \"$SU_TOKEN\",
    \"delegatee\": \"$DELEGATEE_TOKEN\",
    \"target_agent\": \"echo-agent\",
    \"target_capability\": \"echoRespond\"
  }" | jq .
echo

# --- Step 11: Delegatee tries again (should fail - grant revoked) ---
echo ">>> Step 11: Delegatee tries again (should fail - grant revoked)"
HTTP_CODE=$(curl -s -o /tmp/invoke_revoked.txt -w "%{http_code}" -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $DELEGATEE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoRespond","args":{"message":"hello after revoke"}}')
echo "HTTP status: $HTTP_CODE"
echo "Response:"
cat /tmp/invoke_revoked.txt | jq . 2>/dev/null || cat /tmp/invoke_revoked.txt
echo

echo "=== Demo Complete ==="
