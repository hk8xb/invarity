# Invarity

A security control plane for AI agent tool execution. Invarity intercepts tool calls from AI agents, validates them against registered tool schemas, evaluates organizational policies, and runs LLM-based semantic checks before allowing execution.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AI Agent                                        │
│                                  │                                           │
│                                  ▼                                           │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                        Invarity Firewall                               │  │
│  │                                                                        │  │
│  │   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐            │  │
│  │   │ S0: Canon &  │ -> │ S1: Schema   │ -> │ S2: Risk     │            │  │
│  │   │ Bounds Check │    │ Validation   │    │ Compute      │            │  │
│  │   └──────────────┘    └──────────────┘    └──────────────┘            │  │
│  │          │                                       │                     │  │
│  │          v                                       v                     │  │
│  │   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐            │  │
│  │   │ S3: Policy   │ -> │ S4: Alignment│ -> │ S5: Threat   │            │  │
│  │   │ Pass 1       │    │ Quorum (3x)  │    │ Sentinel     │            │  │
│  │   └──────────────┘    └──────────────┘    └──────────────┘            │  │
│  │                              │                   │                     │  │
│  │                              v                   v                     │  │
│  │   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐            │  │
│  │   │ S6: Policy   │ -> │ S7: Policy   │ -> │ S8: Aggregate│ -> Decision│  │
│  │   │ Arbiter      │    │ Pass 2       │    │ Decision     │            │  │
│  │   └──────────────┘    └──────────────┘    └──────────────┘            │  │
│  │                                                                        │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                  │                                           │
│                                  ▼                                           │
│                       ALLOW / ESCALATE / DENY                                │
│                                  │                                           │
│                                  ▼                                           │
│                           Tool Execution                                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Packages

| Package | Description | Language |
|---------|-------------|----------|
| [invarity-go](./invarity-go) | Firewall server with 8-step decision pipeline | Go |
| [invarity-cli](./invarity-cli) | CLI for managing tools, toolsets, and principals | Go |
| [invarity-infra](./invarity-infra) | AWS CDK infrastructure (multi-tenant SaaS) | TypeScript |

## Concepts

Invarity uses a clear ontology for managing AI agent permissions:

- **Tenants**: Organizations using the platform
- **Tools**: Individual tool definitions with schemas and risk metadata (registered to tenant library)
- **Toolsets**: Bundles of tool references (registered to tenant library)
- **Principals**: Agents or services that execute tools (toolsets are applied to principals)

This separation allows tools to be defined once and reused across multiple toolsets and principals.

## Quick Start

### Prerequisites

- Go 1.22+
- Node.js 18+ (for infrastructure)
- Docker (optional)

### Run the Firewall Server

```bash
cd invarity-go
cp .env.example .env
make run
```

Server starts on `http://localhost:8080`.

### Install the CLI

```bash
cd invarity-cli
go install ./cmd/invarity
invarity version
```

### Register Tools and Evaluate

```bash
# Health check
invarity ping

# Register tools to tenant library
invarity tools register-dir ./tools

# Register a toolset
invarity toolsets register -f toolset.yaml --tools-dir ./tools

# Apply toolset to a principal (agent)
invarity principals apply-toolset \
  --principal my-agent \
  --toolset payments-v1 \
  --revision 1.0.0

# Simulate a tool call evaluation
invarity simulate -f request.json --explain
```

## Decision Pipeline

The firewall evaluates each tool call through an 8-step pipeline:

| Step | Name | Type | Description |
|------|------|------|-------------|
| S0 | Canonicalize | Deterministic | Bounds-check, truncate strings, validate required fields |
| S1 | Schema Validation | Deterministic | Verify tool exists, version matches, args conform to schema |
| S2 | Risk Compute | Deterministic | Calculate risk from tool profile + arguments + constraints |
| S3 | Policy Pass 1 | Deterministic | Evaluate policy rules without derived facts |
| S4 | Alignment Quorum | LLM (Always) | 3-voter intention alignment check |
| S5 | Threat Sentinel | LLM (Conditional) | Threat classification when risk >= MEDIUM |
| S6 | Policy Arbiter | LLM (Conditional) | Fact derivation only (never makes decisions) |
| S7 | Policy Pass 2 | Deterministic | Re-evaluate policy with derived facts |
| S8 | Aggregate | Deterministic | Final decision: DENY > ESCALATE > ALLOW |

### Decision Logic

```
DENY > ESCALATE > ALLOW

DENY triggered by:
  - Policy rule denies action
  - Alignment quorum votes DENY
  - Threat sentinel classifies as MALICIOUS
  - Tool constraints violated

ESCALATE triggered by:
  - Alignment quorum votes ESCALATE
  - Threat sentinel classifies as SUSPICIOUS
  - Policy status is UNCOVERED
  - Tool requires human review

ALLOW:
  - All checks pass
```

## API

### Evaluate a Tool Call

```bash
POST /v1/firewall/evaluate
Content-Type: application/json

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
      "amount": 50.00
    }
  }
}
```

### Response

```json
{
  "request_id": "req-abc123",
  "audit_id": "audit-xyz789",
  "decision": "ALLOW",
  "base_risk": "MEDIUM",
  "reasons": ["money_movement", "production_environment"],
  "alignment": {
    "decision": "ALLOW",
    "votes": [
      {"voter_id": "safety_advocate", "vote": "ALLOW", "confidence": 0.92},
      {"voter_id": "intent_verifier", "vote": "ALLOW", "confidence": 0.88},
      {"voter_id": "policy_guardian", "vote": "ALLOW", "confidence": 0.95}
    ]
  },
  "timing": {
    "s4_alignment_ms": 180,
    "total_ms": 245
  }
}
```

## Tool Schema (v3)

Tools follow a conventional format (compatible with OpenAI/Claude) with an `invarity` block for firewall metadata:

```yaml
name: stripe.refund_payment
description: Refund a Stripe payment

parameters:
  type: object
  additionalProperties: false
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
    tags: ["financial", "pii"]

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

## Toolset Schema

Toolsets bundle tool references for assignment to principals:

```yaml
toolset_id: payments-v1
revision: "1.0.0"
display_name: Payments Toolset

tools:
  - tool_id: stripe.refund_payment
    version: 1.0.0
  - tool_id: stripe.create_charge
    version: 1.0.0

labels:
  team: payments
  owner: team@example.com
```

## CLI Commands

```
invarity
├── ping                           # Health check
├── simulate                       # Evaluate tool call
├── version                        # Display version
│
├── tools
│   ├── validate                   # Validate tool manifest
│   ├── register                   # Register tool to tenant library
│   ├── validate-dir               # Batch validate directory
│   └── register-dir               # Batch register directory
│
├── toolsets
│   ├── validate                   # Validate toolset manifest
│   ├── register                   # Register toolset to tenant library
│   └── lint                       # Lint toolset references against tools
│
├── principals
│   └── apply-toolset              # Apply toolset to a principal
│
└── audit
    └── show                       # Retrieve audit record
```

## Configuration

### CLI Configuration

The CLI loads configuration from (in order of precedence):

1. Command-line flags (`--server`, `--api-key`, `--tenant`, `--principal`)
2. Environment variables
3. `~/.invarity/config.yaml`
4. Defaults

```yaml
# ~/.invarity/config.yaml
server: https://api.invarity.dev
api_key: your-api-key-here
tenant_id: acme
principal_id: my-agent
```

Environment variables:
- `INVARITY_SERVER`
- `INVARITY_API_KEY`
- `INVARITY_TENANT_ID`
- `INVARITY_PRINCIPAL_ID`

### Server Configuration

```bash
# Server
PORT=8080
LOG_LEVEL=info

# LLM Endpoints (OpenAI-compatible)
FUNCTIONGEMMA_BASE_URL=http://localhost:8001/v1
FUNCTIONGEMMA_API_KEY=
LLAMAGUARD_BASE_URL=http://localhost:8002/v1
LLAMAGUARD_API_KEY=
QWEN_BASE_URL=http://localhost:8003/v1
QWEN_API_KEY=

# Request Limits
REQUEST_MAX_BYTES=1048576
MAX_CONTEXT_CHARS=32000
MAX_INTENT_CHARS=4000
CACHE_TTL_SECONDS=300

# Feature Flags
ENABLE_THREAT_SENTINEL=true
ENABLE_POLICY_ARBITER=true
```

## LLM Integration

The firewall uses three self-hosted LLM services via OpenAI-compatible APIs:

| Model | Purpose | Called |
|-------|---------|--------|
| **FunctionGemma** | 3-voter alignment quorum | Always |
| **Llama Guard 3** | Threat classification | When risk >= MEDIUM |
| **Qwen** | Fact derivation for policy | When needed by policy |

## Deployment

### Docker

```bash
cd invarity-go
make docker-build
make docker-run
```

### AWS (CDK)

```bash
cd invarity-infra
npm install

# Bootstrap CDK (one-time)
./scripts/bootstrap.sh

# Deploy to dev
./scripts/deploy-dev.sh
```

Infrastructure includes:
- VPC with public/private subnets (2 AZs)
- Application Load Balancer
- ECS Fargate service
- DynamoDB tables (8 tables for multi-tenant data)
- S3 buckets (manifests and audit logs)
- Cognito User Pool for authentication
- KMS encryption
- CloudWatch logging

### DynamoDB Tables

| Table | Purpose |
|-------|---------|
| `tenants` | Tenant configuration |
| `principals` | API principals (agents/services) per tenant |
| `tools-v2` | Tool definitions (immutable versions) |
| `toolsets-v2` | Toolset configurations (immutable revisions) |
| `audit-index` | Audit log index |
| `users` | Cognito user mirror |
| `tenant-memberships` | User to tenant role mappings |
| `tokens` | Developer and agent runtime tokens |

## Development

### Build

```bash
# CLI
cd invarity-cli && go build ./cmd/invarity

# Server
cd invarity-go && make build

# Infrastructure
cd invarity-infra && npm run build
```

### Test

```bash
# CLI
cd invarity-cli && go test ./...

# Server
cd invarity-go && make test-coverage
```

### Lint

```bash
cd invarity-cli && make lint
cd invarity-go && make lint
```

## Project Structure

```
invarity/
├── invarity-cli/
│   ├── cmd/invarity/              # CLI entry point
│   ├── internal/
│   │   ├── cli/                   # Command implementations (cobra)
│   │   ├── client/                # HTTP client
│   │   ├── config/                # Configuration loading
│   │   ├── poller/                # Async polling utilities
│   │   └── validate/              # JSON Schema validation
│   ├── schemas/                   # Tool and toolset JSON schemas
│   └── examples/                  # Example manifests
│
├── invarity-go/
│   ├── cmd/server/                # Server entry point
│   ├── internal/
│   │   ├── audit/                 # Audit record storage
│   │   ├── config/                # Environment configuration
│   │   ├── firewall/              # 8-step decision pipeline
│   │   ├── http/                  # Handlers and router (chi)
│   │   ├── llm/                   # LLM clients (alignment, threat, arbiter)
│   │   ├── policy/                # Policy storage and evaluation
│   │   ├── registry/              # Tool registry and schema validation
│   │   ├── risk/                  # Deterministic risk computation
│   │   ├── types/                 # Shared domain types
│   │   └── util/                  # Utilities
│   └── test/                      # Unit tests
│
└── invarity-infra/
    ├── bin/                       # CDK app entry point
    ├── lib/                       # Stack definitions
    │   ├── firewall-stack.ts      # Main infrastructure stack
    │   └── identity.ts            # Identity construct (Cognito + DynamoDB)
    ├── env/                       # Environment configs (dev.json, prod.json)
    ├── scripts/                   # Deployment scripts
    └── .github/workflows/         # CI/CD workflows
```

## License

Proprietary - Invarity Inc.
