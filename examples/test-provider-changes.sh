#!/usr/bin/env bash
#
# Automated end-to-end test for provider changes.
# Starts orchestrator, agent, display, cron, runs all invocations,
# then tears everything down and checks for clean shutdown.
#
# Usage:  ./examples/test-provider-changes.sh
#
set -euo pipefail

PASS=0
FAIL=0
PIDS=()

pass() { echo "  [PASS] $1"; PASS=$((PASS+1)); }
fail() { echo "  [FAIL] $1"; FAIL=$((FAIL+1)); }
cleanup() {
  echo
  echo "--- Cleanup: sending SIGTERM to all background services ---"
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  # Give them a moment to exit
  sleep 2
  # Check clean shutdown (fix #5): none should still be running
  local hung=0
  for pid in "${PIDS[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      echo "  [FAIL] PID $pid did not exit after SIGTERM (fix #5: stoppable readLoop)"
      FAIL=$((FAIL+1))
      hung=1
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
  if [ "$hung" -eq 0 ]; then
    pass "All services exited cleanly after SIGTERM (fix #5)"
  fi

  echo
  echo "========================================="
  echo "  Results: $PASS passed, $FAIL failed"
  echo "========================================="
  if [ "$FAIL" -gt 0 ]; then exit 1; fi
}
trap cleanup EXIT

BASE="http://localhost:8080"

echo "=== Building binaries ==="
go build -o /tmp/mod-orchestrator ./cmd/orchestrator
go build -o /tmp/mod-agent       ./cmd/agent
go build -o /tmp/mod-display     ./cmd/display
go build -o /tmp/mod-cron        ./cmd/cron
go build -o /tmp/mod-client      ./cmd/client
echo "  Build OK"
echo

# --- Start orchestrator ---
echo "=== Starting orchestrator ==="
/tmp/mod-orchestrator > /tmp/mod-orchestrator.log 2>&1 &
PIDS+=($!)
sleep 2
if curl -sf "$BASE/capabilities" > /dev/null 2>&1; then
  pass "Orchestrator is up"
else
  fail "Orchestrator not reachable"
  exit 1
fi

# --- Start agent (tests fix #3: Run) ---
echo "=== Starting echo-agent ==="
/tmp/mod-agent -name echo-agent > /tmp/mod-agent.log 2>&1 &
PIDS+=($!)
sleep 2

# --- Start display via ORCHESTRATOR_URL env var (tests fix #1) ---
echo "=== Starting display (ORCHESTRATOR_URL env var, fix #1) ==="
ORCHESTRATOR_URL="$BASE" /tmp/mod-display -name test-display > /tmp/mod-display.log 2>&1 &
PIDS+=($!)
sleep 2

# --- Claim tokens and set up policies before starting cron ---
echo
echo "=== Setting up SU token, cron token, and policies ==="
SU_TOKEN=$(curl -sf -X POST "$BASE/su/token" | jq -r '.token')
if [ -n "$SU_TOKEN" ] && [ "$SU_TOKEN" != "null" ]; then
  pass "SU token claimed"
else
  fail "Could not claim SU token"
  exit 1
fi

CRON_TOKEN=$(curl -sf -X POST "$BASE/token" | jq -r '.token')
if [ -n "$CRON_TOKEN" ] && [ "$CRON_TOKEN" != "null" ]; then
  pass "Cron token claimed"
else
  fail "Could not claim cron token"
  exit 1
fi

# Role for SU: can invoke echo-agent caps + deferredCall
curl -sf -X POST "$BASE/policy/role" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test_all",
    "rules": [
      {"service_id": "echo-agent", "capability": "echoNoReturn", "effect": "allow"},
      {"service_id": "echo-agent", "capability": "echoRespond", "effect": "allow"},
      {"service_id": "echo-agent", "capability": "echoTimeout", "effect": "allow"},
      {"service_id": "cron-service", "capability": "deferredCall", "effect": "allow"}
    ]
  }' > /dev/null
curl -sf -X POST "$BASE/policy" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"identity\": \"$SU_TOKEN\",
    \"roles\": [\"test_all\"],
    \"rules\": []
  }" > /dev/null

# Role for cron: needs to invoke echo-agent/echoRespond when the deferred call fires
curl -sf -X POST "$BASE/policy/role" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "cron_invoker",
    "rules": [
      {"service_id": "echo-agent", "capability": "echoRespond", "effect": "allow"}
    ]
  }' > /dev/null
curl -sf -X POST "$BASE/policy" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"identity\": \"$CRON_TOKEN\",
    \"roles\": [\"cron_invoker\"],
    \"rules\": []
  }" > /dev/null
pass "Policies configured (SU + cron)"

# --- Start cron with its token and single -server flag (tests fix #8) ---
echo
echo "=== Starting cron (single -server flag, fix #8) ==="
/tmp/mod-cron -server "$BASE" -token "$CRON_TOKEN" > /tmp/mod-cron.log 2>&1 &
PIDS+=($!)
sleep 2

# --- Verify capabilities registered ---
echo
echo "=== Test: capability registration ==="
CAPS=$(curl -sf "$BASE/capabilities")

if echo "$CAPS" | grep -q "echoNoReturn"; then
  pass "echo-agent capabilities registered (fix #3: agent Run works)"
else
  fail "echo-agent capabilities not found"
fi

if echo "$CAPS" | grep -q "deferredCall"; then
  pass "cron-service registered with single -server flag (fix #8)"
else
  fail "cron-service capabilities not found"
fi

# --- Test invocations ---
echo
echo "=== Test: invoke echoNoReturn ==="
RESP=$(curl -sf -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoNoReturn","args":{"message":"hello"}}')
INV_ID=$(echo "$RESP" | jq -r '.invocation_id')
if [ -n "$INV_ID" ] && [ "$INV_ID" != "null" ]; then
  pass "echoNoReturn invoked (id: ${INV_ID:0:12}...)"
else
  fail "echoNoReturn invoke failed: $RESP"
fi

sleep 1

echo
echo "=== Test: invoke echoRespond + get result ==="
RESP=$(curl -sf -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"echo-agent","function_name":"echoRespond","args":{"message":"provider test"}}')
INV_ID=$(echo "$RESP" | jq -r '.invocation_id')
if [ -n "$INV_ID" ] && [ "$INV_ID" != "null" ]; then
  sleep 1
  RESULT=$(curl -sf "$BASE/invoke/result/$INV_ID" \
    -H "Authorization: Bearer $SU_TOKEN")
  if echo "$RESULT" | grep -q "provider test"; then
    pass "echoRespond returned correct result"
  else
    fail "echoRespond result unexpected: $RESULT"
  fi
else
  fail "echoRespond invoke failed: $RESP"
fi

echo
echo "=== Test: display received messages (fix #1 + #3) ==="
sleep 1
if grep -q "EchoNoReturn\|EchoRespond" /tmp/mod-display.log 2>/dev/null; then
  pass "Display received agent messages (env var + Run work)"
else
  fail "Display log has no agent messages"
  echo "  Display log contents:"
  cat /tmp/mod-display.log 2>/dev/null || echo "  (empty)"
fi

echo
echo "=== Test: invoke deferredCall on cron ==="
RESP=$(curl -sf -X POST "$BASE/invoke" \
  -H "Authorization: Bearer $SU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_name":"cron-service",
    "function_name":"deferredCall",
    "args":{"target_agent":"echo-agent","target_capability":"echoRespond","target_args":{"message":"deferred hello"},"delay_seconds":2}
  }')
INV_ID=$(echo "$RESP" | jq -r '.invocation_id')
if [ -n "$INV_ID" ] && [ "$INV_ID" != "null" ]; then
  pass "deferredCall accepted by cron"
  # Wait for the deferred execution (2s delay + margin)
  sleep 4
  if grep -q "executing deferred call" /tmp/mod-cron.log 2>/dev/null; then
    pass "Cron executed deferred call after delay"
  else
    fail "Cron did not execute deferred call"
  fi
  # Check the display log: cron sends a display message with level "success" or "error"
  # Display format is:
  #   Title : Deferred Call: echo-agent/echoRespond
  #   Level : success       <-- or "error" if invoke was denied
  if grep -A1 "Deferred Call" /tmp/mod-display.log 2>/dev/null | grep -q "success"; then
    pass "Cron's re-invocation of echo-agent succeeded (display shows success)"
  else
    fail "Cron's re-invocation of echo-agent failed (policy denied or error)"
    echo "  Display log around deferred call:"
    grep -B2 -A3 "Deferred Call" /tmp/mod-display.log 2>/dev/null || echo "  (no Deferred Call in display log)"
  fi
else
  fail "deferredCall invoke failed: $RESP"
fi

echo
echo "=== Test: cmd/client GetInvokeResultCtx (fix #4) ==="
CLIENT_OUT=$(/tmp/mod-client -agent echo-agent -token "$SU_TOKEN" 2>&1 || true)

if echo "$CLIENT_OUT" | grep -q "success=true"; then
  pass "cmd/client got successful results via GetInvokeResultCtx"
else
  fail "cmd/client did not get successful results"
  echo "  Output: $CLIENT_OUT"
fi

if echo "$CLIENT_OUT" | grep -q "timed out"; then
  pass "cmd/client echoTimeout correctly timed out via context"
else
  fail "cmd/client echoTimeout did not time out as expected"
fi

echo
echo "=== Test: clean shutdown (fix #5) ==="
echo "  (checked during cleanup via SIGTERM)"