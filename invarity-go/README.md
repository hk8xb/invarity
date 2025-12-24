# Invarity Firewall

The **Invarity Firewall** is a security control-plane server that intercepts proposed tool calls from AI agents, runs deterministic validation and policy evaluation, calls self-hosted LLM services for semantic checks, aggregates decisions, and logs auditable decision records.

## Features

- **Deterministic Pipeline**: 8-step decision pipeline with clear state transitions
- **Multi-Model Alignment**: 3-voter quorum using FunctionGemma for intention alignment
- **Threat Detection**: Llama Guard 3 for adversarial threat classification
- **Policy Arbitration**: Qwen for fact derivation (not decision-making)
- **Auditable**: Complete audit trail for every decision
- **Extensible**: Clean interfaces for registry, policy, and audit stores

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Invarity Firewall                                │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐ │
│  │ S0: Canon.  │ → │ S1: Schema  │ → │ S2: Risk    │ → │ S3: Policy  │ │
│  │ & Bounds    │   │ Validation  │   │ Compute     │   │ Pass 1      │ │
│  └─────────────┘   └─────────────┘   └─────────────┘   └─────────────┘ │
│         │                │                  │                │         │
│         ↓                ↓                  ↓                ↓         │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐ │
│  │ S4: Align.  │ → │ S5: Threat  │ → │ S6: Policy  │ → │ S7: Policy  │ │
│  │ Quorum      │   │ Sentinel    │   │ Arbiter     │   │ Pass 2      │ │
│  │ (3 voters)  │   │ (cond.)     │   │ (cond.)     │   │ (cond.)     │ │
│  └─────────────┘   └─────────────┘   └─────────────┘   └─────────────┘ │
│                                                                │         │
│                                                                ↓         │
│                                                        ┌─────────────┐ │
│                                                        │ S8: Agg.    │ │
│                                                        │ Decision    │ │
│                                                        └─────────────┘ │
│                                                                │         │
│                                                                ↓         │
│                                                         ALLOW/ESCALATE/  │
│                                                              DENY        │
└─────────────────────────────────────────────────────────────────────────┘
```

## Pipeline Steps

| Step | Name | Type | Description |
|------|------|------|-------------|
| S0 | Canonicalize | Deterministic | Bounds-check, truncate, validate required fields |
| S1 | Schema Validation | Deterministic | Verify tool exists, version matches, args validate |
| S2 | Risk Compute | Deterministic | Calculate base risk from tool profile + args |
| S3 | Policy Pass 1 | Deterministic | Evaluate policy rules (no derived facts) |
| S4 | Alignment Quorum | LLM (Always) | 3-voter intention alignment check |
| S5 | Threat Sentinel | LLM (Conditional) | Threat classification (risk >= MEDIUM) |
| S6 | Policy Arbiter | LLM (Conditional) | Fact derivation only (risk >= MEDIUM + needs facts) |
| S7 | Policy Pass 2 | Deterministic | Re-evaluate with derived facts |
| S8 | Aggregate | Deterministic | Final decision: DENY > ESCALATE > ALLOW |

## Decision Logic

- **Any DENY → DENY** (from policy, alignment, or threat)
- **Any ESCALATE → ESCALATE** (from alignment, threat suspicious, or policy uncovered)
- **Otherwise → ALLOW**

**Important**: The Policy Arbiter (S6) derives facts only. It does NOT make ALLOW/DENY decisions. Final authority is always the deterministic policy evaluation.

## Quick Start

### Prerequisites

- Go 1.22+
- (Optional) Docker for containerized deployment
- (Optional) Self-hosted LLM endpoints

### Build and Run

```bash
# Download dependencies
make deps

# Build
make build

# Run locally (uses in-memory stores and mock LLMs)
make run

# Run tests
make test
```

### Configuration

Set environment variables or create a `.env` file:

```bash
# Server
PORT=8080
LOG_LEVEL=info

# AWS (for S3 stores - optional in MVP)
S3_BUCKET=invarity-data
AWS_REGION=us-east-1

# LLM Endpoints (OpenAI-compatible)
FUNCTIONGEMMA_BASE_URL=http://localhost:8001/v1
FUNCTIONGEMMA_API_KEY=your-api-key
LLAMAGUARD_BASE_URL=http://localhost:8002/v1
LLAMAGUARD_API_KEY=your-api-key
QWEN_BASE_URL=http://localhost:8003/v1
QWEN_API_KEY=your-api-key

# Limits
REQUEST_MAX_BYTES=1048576
MAX_CONTEXT_CHARS=32000
MAX_INTENT_CHARS=4000

# Cache
CACHE_TTL_SECONDS=300
```

### API Endpoints

#### POST /v1/firewall/evaluate

Evaluate a tool call request.

**Request:**
```json
{
  "org_id": "acme-corp",
  "actor": {
    "id": "user-123",
    "role": "developer",
    "type": "user",
    "org_id": "acme-corp"
  },
  "env": "production",
  "user_intent": "Transfer $500 to the marketing budget account",
  "tool_call": {
    "action_id": "transfer_funds",
    "version": "1.0.0",
    "args": {
      "from_account": "ops-budget",
      "to_account": "marketing-budget",
      "amount": 500,
      "currency": "USD",
      "memo": "Q4 reallocation"
    }
  },
  "bounded_context": {
    "conversation_history": [
      "User: Can you help me move some budget around?",
      "Agent: Sure, which accounts are involved?"
    ]
  }
}
```

**Response:**
```json
{
  "request_id": "req-abc123",
  "audit_id": "audit-xyz789",
  "decision": "ALLOW",
  "base_risk": "MEDIUM",
  "reasons": ["money_movement", "production_environment"],
  "policy": {
    "version": "1.0.0",
    "status": "COVERED",
    "matched_rules": ["allow-internal-transfers"]
  },
  "alignment": {
    "voters": [
      {"voter_id": "safety_advocate", "vote": "ALLOW", "confidence": 0.92},
      {"voter_id": "intent_verifier", "vote": "ALLOW", "confidence": 0.88},
      {"voter_id": "policy_guardian", "vote": "ALLOW", "confidence": 0.95}
    ],
    "aggregated_vote": "ALLOW"
  },
  "timing": {
    "total_ms": 245,
    "alignment_ms": 180
  },
  "evaluated_at": "2024-01-15T10:30:00Z"
}
```

#### GET /healthz

Liveness probe.

#### GET /readyz

Readiness probe with dependency checks.

## Sample Requests

### Simple Read (ALLOW expected)
```bash
curl -X POST http://localhost:8080/v1/firewall/evaluate \
  -H "Content-Type: application/json" \
  -d '{
    "org_id": "test-org",
    "actor": {"id": "user-123", "role": "developer", "type": "user", "org_id": "test-org"},
    "env": "development",
    "user_intent": "Read the configuration file",
    "tool_call": {
      "action_id": "read_file",
      "version": "1.0.0",
      "args": {"path": "/etc/config.json"}
    }
  }'
```

### Subtle Misalignment (ESCALATE expected)

This demonstrates a scenario where the user intent doesn't match the tool action:

```bash
curl -X POST http://localhost:8080/v1/firewall/evaluate \
  -H "Content-Type: application/json" \
  -d '{
    "org_id": "test-org",
    "actor": {"id": "user-123", "role": "developer", "type": "user", "org_id": "test-org"},
    "env": "production",
    "user_intent": "Check the user account balance",
    "tool_call": {
      "action_id": "transfer_funds",
      "version": "1.0.0",
      "args": {
        "from_account": "user-123-savings",
        "to_account": "external-account-xyz",
        "amount": 5000,
        "currency": "USD",
        "memo": "routine check"
      }
    },
    "fuzzy_context": true
  }'
```

Expected result: **ESCALATE** because:
1. User intent says "check balance" but action is "transfer funds" (misalignment)
2. Destination is an external account (suspicious)
3. High-risk money movement in production
4. Fuzzy context flag triggers arbiter

## LLM Endpoints

The firewall uses three self-hosted LLM services via OpenAI-compatible APIs:

### FunctionGemma (Alignment Quorum)
- **Purpose**: 3-voter intention alignment check
- **Model**: FunctionGemma (or similar function-calling model)
- **Called**: Always (every request)
- **Output**: Vote (ALLOW/ESCALATE/DENY), confidence, reason codes

### Llama Guard 3 (Threat Sentinel)
- **Purpose**: Security threat classification
- **Model**: Llama Guard 3
- **Called**: When base_risk >= MEDIUM
- **Output**: Label (CLEAR/SUSPICIOUS/MALICIOUS), threat types

### Qwen (Policy Arbiter)
- **Purpose**: Fact derivation for policy evaluation
- **Model**: Qwen (or similar reasoning model)
- **Called**: When base_risk >= MEDIUM AND (policy needs facts OR fuzzy context)
- **Output**: Derived facts with confidence (NO decisions)

### Connecting to RunPod/vLLM

Example for connecting to a vLLM instance on RunPod:

```bash
# RunPod vLLM endpoint format
FUNCTIONGEMMA_BASE_URL=https://your-pod-id-8000.proxy.runpod.net/v1
FUNCTIONGEMMA_API_KEY=your-runpod-api-key
```

The server expects OpenAI-compatible `/chat/completions` endpoints with JSON mode support.

## Project Structure

```
invarity/
├── cmd/
│   └── server/
│       └── main.go           # Entry point
├── internal/
│   ├── audit/
│   │   └── audit.go          # Audit record storage
│   ├── config/
│   │   └── config.go         # Configuration
│   ├── firewall/
│   │   └── pipeline.go       # Decision pipeline
│   ├── http/
│   │   ├── handlers.go       # HTTP handlers
│   │   └── router.go         # Router setup
│   ├── llm/
│   │   ├── alignment.go      # Alignment quorum
│   │   ├── arbiter.go        # Policy arbiter
│   │   ├── client.go         # OpenAI-compatible client
│   │   └── threat.go         # Threat sentinel
│   ├── policy/
│   │   ├── evaluator.go      # Policy evaluation
│   │   └── policy.go         # Policy storage
│   ├── registry/
│   │   ├── registry.go       # Tool registry
│   │   └── validator.go      # Schema validation
│   ├── risk/
│   │   └── risk.go           # Risk computation
│   ├── types/
│   │   └── types.go          # Shared types
│   └── util/
│       └── util.go           # Utilities
├── test/
│   ├── alignment_test.go
│   ├── canonicalize_test.go
│   ├── decision_test.go
│   ├── policy_test.go
│   └── risk_test.go
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Dependencies

| Package | Purpose |
|---------|---------|
| github.com/go-chi/chi/v5 | HTTP router (lightweight, stdlib-compatible) |
| github.com/google/uuid | UUID generation |
| github.com/santhosh-tekuri/jsonschema/v5 | JSON Schema validation |
| go.uber.org/zap | Structured logging |

## Development

```bash
# Format code
make fmt

# Run linter (requires golangci-lint)
make lint

# Run tests with coverage
make test-coverage

# Build for Linux
make build-linux
```

## TODO / Future Work

- [ ] Implement S3-backed stores (registry, policy, audit)
- [ ] Add DynamoDB index for audit queries
- [ ] Implement actual LLM integration tests with mock server
- [ ] Add OpenTelemetry tracing
- [ ] Add Prometheus metrics
- [ ] Implement rate limiting
- [ ] Add authentication middleware
- [ ] WebSocket support for streaming decisions
- [ ] Policy DSL compiler (separate service)
- [ ] Admin UI for policy management

## License

Proprietary - Invarity Inc.
