# Invarity Firewall

The core firewall server for Invarity - a security control plane for AI agent tool execution. The firewall intercepts tool calls from AI agents, validates them against registered tool schemas, evaluates organizational policies, and runs LLM-based semantic checks before allowing execution.

## Overview

Invarity Firewall evaluates every proposed tool call through an 8-step pipeline that combines deterministic logic with LLM-powered semantic analysis. It ensures agents only perform actions that are:

- **Schema-compliant** with registered tool definitions
- **Aligned** with user intent (3-voter LLM quorum)
- **Safe** from adversarial threats (Llama Guard)
- **Compliant** with organizational policies
- **Auditable** with complete decision trails

## Architecture

```
                           Invarity Firewall
┌──────────────────────────────────────────────────────────────────────────┐
│                                                                          │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│   │ S0: Canon &  │ -> │ S1: Schema   │ -> │ S2: Risk     │              │
│   │ Bounds Check │    │ Validation   │    │ Compute      │              │
│   └──────────────┘    └──────────────┘    └──────────────┘              │
│          │                                       │                       │
│          v                                       v                       │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│   │ S3: Policy   │ -> │ S4: Alignment│ -> │ S5: Threat   │              │
│   │ Pass 1       │    │ Quorum (3x)  │    │ Sentinel     │              │
│   └──────────────┘    └──────────────┘    └──────────────┘              │
│                              │                   │                       │
│                              v                   v                       │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│   │ S6: Policy   │ -> │ S7: Policy   │ -> │ S8: Aggregate│ -> Decision │
│   │ Arbiter      │    │ Pass 2       │    │ Decision     │              │
│   └──────────────┘    └──────────────┘    └──────────────┘              │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
                                    │
                                    v
                          ALLOW / ESCALATE / DENY
```

## Pipeline Steps

| Step | Name | Type | Description |
|------|------|------|-------------|
| S0 | Canonicalize | Deterministic | Bounds-check, truncate strings, validate required fields |
| S1 | Schema Validation | Deterministic | Verify tool exists in registry, version matches, args conform to schema |
| S2 | Risk Compute | Deterministic | Calculate base risk from tool profile + arguments + constraints |
| S3 | Policy Pass 1 | Deterministic | Evaluate policy rules without derived facts |
| S4 | Alignment Quorum | LLM (Always) | 3-voter intention alignment check |
| S5 | Threat Sentinel | LLM (Conditional) | Threat classification when risk >= MEDIUM |
| S6 | Policy Arbiter | LLM (Conditional) | Fact derivation only (never makes decisions) |
| S7 | Policy Pass 2 | Deterministic | Re-evaluate policy with derived facts |
| S8 | Aggregate | Deterministic | Final decision: DENY > ESCALATE > ALLOW |

## Decision Logic

```
DENY > ESCALATE > ALLOW

DENY triggered by:
  - Policy rule denies action
  - Alignment quorum votes DENY
  - Threat sentinel classifies as MALICIOUS
  - Tool constraints violated (amount_limit, required_args, etc.)

ESCALATE triggered by:
  - Alignment quorum votes ESCALATE
  - Threat sentinel classifies as SUSPICIOUS
  - Policy status is UNCOVERED
  - Tool requires human review

ALLOW:
  - All checks pass
```

**Important**: The Policy Arbiter (S6) derives facts only. It does NOT make ALLOW/DENY decisions. Final authority is always the deterministic policy evaluation.

## Quick Start

### Prerequisites

- Go 1.22+
- (Optional) Docker for containerized deployment
- (Optional) Self-hosted LLM endpoints (FunctionGemma, Llama Guard 3, Qwen)

### Build and Run

```bash
# Download dependencies
make deps

# Build
make build

# Run locally (uses in-memory stores)
make run

# Run with .env file
make run-env

# Run tests
make test
```

### Docker

```bash
# Build image
make docker-build

# Run container
make docker-run
```

### Using the CLI

The [invarity-cli](../invarity-cli) provides a convenient interface:

```bash
# Health check
invarity ping

# Register tools to tenant library
invarity tools register-dir ./tools

# Register a toolset
invarity toolsets register -f toolset.yaml --tools-dir ./tools

# Apply toolset to a principal
invarity principals apply-toolset --principal my-agent --toolset payments-v1 --revision 1.0.0

# Simulate a tool call evaluation
invarity simulate -f request.json --explain
```

## Configuration

Copy `.env.example` to `.env` and customize:

```bash
# Server
PORT=8080
LOG_LEVEL=info                    # debug, info, warn, error

# LLM Endpoints (OpenAI-compatible)
FUNCTIONGEMMA_BASE_URL=http://localhost:8001/v1
FUNCTIONGEMMA_API_KEY=
LLAMAGUARD_BASE_URL=http://localhost:8002/v1
LLAMAGUARD_API_KEY=
QWEN_BASE_URL=http://localhost:8003/v1
QWEN_API_KEY=

# Request Limits
REQUEST_MAX_BYTES=1048576         # 1MB max request size
MAX_CONTEXT_CHARS=32000           # Conversation history truncation
MAX_INTENT_CHARS=4000             # User intent truncation

# Cache
CACHE_TTL_SECONDS=300             # Policy cache TTL (5 minutes)

# Feature Flags
ENABLE_THREAT_SENTINEL=true       # Enable/disable threat detection
ENABLE_POLICY_ARBITER=true        # Enable/disable fact derivation

# AWS (for production deployment)
S3_BUCKET=
AWS_REGION=us-east-1
```

### Production Environment Variables

When deployed to AWS (via [invarity-infra](../invarity-infra)), additional environment variables are injected:

| Variable | Description |
|----------|-------------|
| `INVARITY_ENV` | Environment name (dev, prod) |
| `TENANTS_TABLE` | DynamoDB tenants table |
| `PRINCIPALS_TABLE` | DynamoDB principals table |
| `TOOLS_TABLE` | DynamoDB tools table |
| `TOOLSETS_TABLE` | DynamoDB toolsets table |
| `AUDIT_INDEX_TABLE` | DynamoDB audit index table |
| `USERS_TABLE` | DynamoDB users table |
| `TENANT_MEMBERSHIPS_TABLE` | DynamoDB tenant memberships table |
| `TOKENS_TABLE` | DynamoDB tokens table |
| `MANIFESTS_BUCKET` | S3 bucket for tool/toolset manifests |
| `AUDIT_BLOBS_BUCKET` | S3 bucket for audit records |
| `KMS_KEY_ARN` | KMS key for encryption |
| `COGNITO_USER_POOL_ID` | Cognito User Pool ID |
| `COGNITO_ISSUER_URL` | Cognito OIDC Issuer URL |

## API Reference

### Firewall Evaluation

#### POST /v1/firewall/evaluate

Evaluate a proposed tool call against the firewall pipeline.

**Request:**
```json
{
  "org_id": "acme-corp",
  "actor": {
    "id": "agent-123",
    "role": "support-agent",
    "type": "agent",
    "org_id": "acme-corp"
  },
  "env": "production",
  "user_intent": "Process customer refund for damaged item",
  "tool_call": {
    "action_id": "stripe.refund_payment",
    "version": "1.0.0",
    "args": {
      "payment_id": "pi_abc123",
      "amount": 50.00,
      "currency": "USD"
    }
  },
  "bounded_context": {
    "conversation_history": [
      "Customer: I received a damaged product",
      "Agent: I'll process a refund for you"
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
    "matched_rules": ["allow-small-refunds"]
  },
  "alignment": {
    "decision": "ALLOW",
    "votes": [
      {"voter_id": "safety_advocate", "vote": "ALLOW", "confidence": 0.92},
      {"voter_id": "intent_verifier", "vote": "ALLOW", "confidence": 0.88},
      {"voter_id": "policy_guardian", "vote": "ALLOW", "confidence": 0.95}
    ]
  },
  "threat": {
    "label": "CLEAR",
    "threat_types": [],
    "confidence": 0.97
  },
  "timing": {
    "s0_canonicalize_ms": 1,
    "s1_schema_ms": 2,
    "s2_risk_ms": 1,
    "s3_policy_pass1_ms": 3,
    "s4_alignment_ms": 180,
    "s5_threat_ms": 45,
    "total_ms": 245
  },
  "evaluated_at": "2024-01-15T10:30:00Z"
}
```

### Tool Management

#### POST /v1/tenants/{tenant_id}/tools

Register a tool to a tenant's tool library.

**Request:**
```json
{
  "name": "stripe.refund_payment",
  "description": "Refund a Stripe payment",
  "parameters": {
    "type": "object",
    "properties": {
      "payment_id": { "type": "string" },
      "amount": { "type": "number" }
    },
    "required": ["payment_id", "amount"]
  },
  "invarity": {
    "id": "stripe.refund_payment",
    "version": "1.0.0",
    "risk": {
      "base_risk": "high",
      "operation": "write",
      "requires_human_review": false
    },
    "constraints": {
      "requires_justification": true,
      "required_args": ["payment_id", "amount"],
      "disallow_wildcards": true,
      "max_bulk": null,
      "amount_limit": {
        "max": 10000,
        "currency": "USD",
        "arg_key": "amount"
      }
    }
  }
}
```

#### GET /v1/tenants/{tenant_id}/tools/{tool_id}

Retrieve a registered tool.

### Toolset Management

#### POST /v1/tenants/{tenant_id}/toolsets

Register a toolset to a tenant's toolset library.

**Request:**
```json
{
  "toolset_id": "payments-v1",
  "revision": "1.0.0",
  "display_name": "Payments Toolset",
  "tools": [
    { "tool_id": "stripe.refund_payment", "version": "1.0.0" },
    { "tool_id": "stripe.create_charge", "version": "1.0.0" }
  ]
}
```

### Principal Management

#### POST /v1/tenants/{tenant_id}/principals/{principal_id}/toolsets

Apply a toolset to a principal.

**Request:**
```json
{
  "toolset_id": "payments-v1",
  "revision": "1.0.0"
}
```

### Health Endpoints

#### GET /healthz

Liveness probe.

```json
{"status": "ok", "timestamp": "2024-01-15T10:30:00Z"}
```

#### GET /readyz

Readiness probe with dependency checks.

```json
{
  "status": "ready",
  "checks": {
    "registry": "ok",
    "policy": "ok",
    "llm": "ok"
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

## Data Model

### Multi-Tenant Architecture

The firewall uses a multi-tenant data model:

- **Tenants**: Organizations using the platform
- **Principals**: Agents or services that execute tools (scoped to tenant)
- **Tools**: Tool definitions with schemas and risk metadata (scoped to tenant)
- **Toolsets**: Bundles of tools applied to principals (scoped to tenant)

### Tool Schema (v3)

Tools follow a conventional format (compatible with OpenAI/Claude) with an `invarity` block:

```yaml
name: stripe.refund_payment
description: Refund a Stripe payment

parameters:
  type: object
  properties:
    payment_id:
      type: string
    amount:
      type: number
  required:
    - payment_id
    - amount

invarity:
  id: stripe.refund_payment
  version: 1.0.0

  risk:
    base_risk: high           # low, medium, high, critical
    operation: write          # read, write, delete, execute
    requires_human_review: false
    tags: ["financial"]

  constraints:
    requires_justification: true
    required_args: ["payment_id", "amount"]
    disallow_wildcards: true
    max_bulk: null
    amount_limit:
      max: 10000
      currency: USD
      arg_key: amount
```

### Toolset Schema

Toolsets bundle tool references:

```yaml
toolset_id: payments-v1
revision: "1.0.0"
display_name: Payments Toolset

tools:
  - tool_id: stripe.refund_payment
    version: 1.0.0
  - tool_id: stripe.create_charge
    version: 1.0.0
```

## LLM Integration

The firewall uses three self-hosted LLM services via OpenAI-compatible APIs:

| Model | Purpose | Called |
|-------|---------|--------|
| **FunctionGemma** | 3-voter alignment quorum | Always |
| **Llama Guard 3** | Threat classification | When risk >= MEDIUM |
| **Qwen** | Fact derivation for policy | When needed by policy |

### Alignment Quorum (FunctionGemma)

Three voters evaluate every request:
1. **Safety Advocate** - Prioritizes user safety (temp: 0.1)
2. **Intent Verifier** - Verifies tool matches intent (temp: 0.2)
3. **Policy Guardian** - Evaluates org policy alignment (temp: 0.15)

Each outputs: `vote` (ALLOW/ESCALATE/DENY), `confidence`, `reason_codes`

### Threat Sentinel (Llama Guard 3)

Detects adversarial patterns:
- Prompt injection
- Data exfiltration
- Privilege escalation
- Social engineering
- Unauthorized access
- Malicious payloads

Labels: `CLEAR`, `SUSPICIOUS`, `MALICIOUS`

### Policy Arbiter (Qwen)

Derives facts needed by policy rules. **Does not make decisions** - only provides structured facts with confidence scores for deterministic policy evaluation.

### Connecting to RunPod/vLLM

```bash
FUNCTIONGEMMA_BASE_URL=https://your-pod-id-8000.proxy.runpod.net/v1
FUNCTIONGEMMA_API_KEY=your-runpod-api-key
```

All endpoints must support OpenAI-compatible `/chat/completions` with JSON mode.

## Project Structure

```
invarity-go/
├── cmd/server/
│   └── main.go              # Entry point, server initialization
├── internal/
│   ├── audit/               # Audit record storage (DynamoDB, S3)
│   ├── config/              # Environment configuration
│   ├── firewall/            # 8-step decision pipeline
│   ├── http/                # Handlers and router (chi)
│   ├── llm/                 # LLM clients (alignment, threat, arbiter)
│   ├── policy/              # Policy storage and evaluation
│   ├── registry/            # Tool registry and schema validation
│   ├── risk/                # Deterministic risk computation
│   ├── types/               # Shared domain types
│   └── util/                # Utilities (hashing, JSON, etc.)
├── test/                    # Unit tests
├── Dockerfile               # Multi-stage Alpine build
├── Makefile                 # Build, test, run targets
└── .env.example             # Configuration template
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/go-chi/chi/v5` | HTTP router |
| `github.com/google/uuid` | UUID generation |
| `github.com/santhosh-tekuri/jsonschema/v5` | JSON Schema validation |
| `go.uber.org/zap` | Structured logging |

## Development

```bash
make fmt            # Format code
make lint           # Lint (requires golangci-lint)
make test           # Run tests with race detection
make test-coverage  # Generate coverage report
make clean          # Clean build artifacts
make help           # Show all targets
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `build` | Build the binary |
| `build-linux` | Cross-compile for Linux amd64 |
| `run` | Run server locally |
| `run-env` | Run with .env file |
| `test` | Run tests |
| `test-coverage` | Run tests with HTML coverage report |
| `docker-build` | Build Docker image |
| `docker-run` | Run Docker container |
| `sample-request` | Send test ALLOW request |
| `sample-misalignment` | Send test ESCALATE request |
| `health` | Check /healthz |
| `ready` | Check /readyz |

## Deployment

### Local Development

```bash
make run
```

### Docker

```bash
make docker-build
make docker-run
```

### AWS (via CDK)

See [invarity-infra](../invarity-infra) for AWS deployment:

```bash
cd ../invarity-infra
./scripts/deploy-dev.sh
```

Infrastructure includes:
- VPC with public/private subnets
- Application Load Balancer
- ECS Fargate service
- DynamoDB tables (8 tables for multi-tenant data)
- S3 buckets (manifests and audit logs)
- Cognito for user authentication
- KMS encryption

## Related Packages

| Package | Description |
|---------|-------------|
| [invarity-cli](../invarity-cli) | CLI for managing tools, toolsets, and policies |
| [invarity-infra](../invarity-infra) | AWS CDK infrastructure |

## Roadmap

- [ ] S3-backed tool/toolset storage
- [ ] DynamoDB audit indexing
- [ ] OpenTelemetry tracing
- [ ] Prometheus metrics
- [ ] Rate limiting
- [ ] Token-based authentication
- [ ] WebSocket streaming decisions
- [ ] Policy DSL compiler
- [ ] Admin UI for policy management

## License

Proprietary - Invarity Inc.
