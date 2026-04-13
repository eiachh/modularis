# Cron Service Demo Guide

This guide shows how to run the cron service demo manually using the CLI.

## Prerequisites

Start the services in separate terminals:

```bash
# Terminal 1: Orchestrator
go run ./cmd/orchestrator

# Terminal 2: Echo Agent
go run ./cmd/agent -name echo-agent

# Terminal 3: Cron Service (auto-fetches token)
go run ./cmd/cron

# Terminal 4: CLI
go run ./cmd/cli
```

## CLI Steps

When you start the CLI, it will auto-fetch the SU token. Follow these steps:

### Step 1: Create Role `echo_user`

1. Select option **7** (Create role)
2. Role name: `echo_user`
3. Add rule? `y`
4. service_id: `echo-agent`
5. capability: `echoRespond`
6. effect: `allow`
7. Add rule? `n`

### Step 2: Create Policy for SU Token

1. Select option **8** (Create policy)
2. Identity: press **Enter** (uses SU token)
3. Select role by index: `0` (selects echo_user)
4. Add direct rule? `n`

### Step 3: Create Delegation Grant

1. Select option **10** (Create delegation grant)
2. Delegator identity: press **Enter** (uses SU token)
3. Delegatee identity: select the **cron-service token** (look for the non-SU token, usually index 1)
4. Target agent name: `echo-agent`
5. Target capability name: `echoRespond`
6. Expiry Unix timestamp: `0`
7. Confirm: `yes`

### Step 4: Invoke deferredCall

1. Select option **3** (Invoke capability)
2. Agent name: `cron-service`
3. Capability name: `deferredCall`
4. Path to JSON args file: `data/deferred-call.json`

### Step 5: Observe

Watch the orchestrator and cron-service logs. After 3 seconds, you should see:
- The cron service executing the deferred call
- The echo-agent receiving the message
- The result broadcast to any connected displays

## JSON Args Files

The `data/` directory contains pre-made JSON files for testing:

- `data/deferred-call.json` - Schedules a call to echo-agent after 3 seconds

You can create custom JSON files with different parameters:

```json
{
  "target_agent": "echo-agent",
  "target_capability": "echoRespond",
  "target_args": {"message": "Your message here"},
  "delay_seconds": 5
}
```

## Environment Variables

All services respect these environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `MODULARIS_HOST` | Orchestrator host | `localhost` |
| `MODULARIS_PORT` | Orchestrator port | `8080` |
| `MODULARIS_SERVER` | Full server URL (overrides host/port) | - |

Example:
```bash
export MODULARIS_PORT=9000
go run ./cmd/orchestrator  # listens on :9000
go run ./cmd/agent -name echo-agent  # connects to localhost:9000
```
