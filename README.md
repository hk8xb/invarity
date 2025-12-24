# Invarity

A security control plane for AI agent tool execution. Invarity intercepts tool calls from AI agents and runs them through a deterministic decision pipeline combining schema validation, policy evaluation, and LLM-based semantic checks.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         AI Agent                                │
│                            │                                    │
│                            ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                   Invarity Firewall                       │  │
│  │  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────────────┐  │  │
│  │  │ Schema │→ │  Risk  │→ │ Policy │→ │ LLM Alignment  │  │  │
│  │  │ Valid. │  │ Compute│  │ Pass 1 │  │ Quorum (3x)    │  │  │
│  │  └────────┘  └────────┘  └────────┘  └────────────────┘  │  │
│  │       │                                      │            │  │
│  │       ▼                                      ▼            │  │
│  │  ┌────────────────┐  ┌────────────┐  ┌────────────────┐  │  │
│  │  │ Threat Sentinel│→ │ Policy     │→ │ Final Decision │  │  │
│  │  │ (Llama Guard)  │  │ Arbiter    │  │ (allow/deny/   │  │  │
│  │  └────────────────┘  └────────────┘  │  escalate)     │  │  │
│  │                                       └────────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
│                            │                                    │
│                            ▼                                    │
│                      Tool Execution                             │
└─────────────────────────────────────────────────────────────────┘
```

## Packages

| Package | Description | Language |
|---------|-------------|----------|
| [invarity-cli](./invarity-cli) | CLI for managing tools, toolsets, and policies | Go |
| [invarity-go](./invarity-go) | Firewall server with decision pipeline | Go |
| [invarity-infra](./invarity-infra) | AWS CDK infrastructure | TypeScript |

## Quick Start

### Prerequisites

- Go 1.22+
- Node.js 18+ (for infrastructure)
- Docker (optional, for containerized deployment)

### Run the Firewall Server Locally

```bash
cd invarity-go
cp .env.example .env
# Configure LLM endpoints in .env
make run
```

The server starts on `http://localhost:8080`.

### Install the CLI

```bash
cd invarity-cli
make install
invarity version
```

### Evaluate a Tool Call

```bash
# Health check
invarity ping

# Simulate a tool call
invarity simulate \
  --tool stripe.refund_payment \
  --arguments '{"payment_id": "pi_123", "amount": 50.00}' \
  --context "Customer requested refund for damaged item"
```

## Decision Pipeline

The firewall evaluates each tool call through an 8-step pipeline:

| Step | Name | Type | Description |
|------|------|------|-------------|
| S0 | Canonicalization | Deterministic | Bounds checking, input normalization |
| S1 | Schema Validation | Deterministic | JSON Schema validation against tool definition |
| S2 | Risk Computation | Deterministic | Calculate risk score from tool metadata |
| S3 | Policy Pass 1 | Deterministic | Evaluate deterministic policy rules |
| S4 | Alignment Quorum | LLM | 3-voter quorum using FunctionGemma |
| S5 | Threat Sentinel | LLM | Threat detection via Llama Guard 3 (conditional) |
| S6 | Policy Arbiter | LLM | Semantic policy evaluation via Qwen (conditional) |
| S7 | Policy Pass 2 | Deterministic | Final deterministic rule pass |
| S8 | Aggregate | - | Combine all signals into final decision |

## API

### Evaluate a Tool Call

```bash
POST /v1/firewall/evaluate
Content-Type: application/json

{
  "tool_id": "stripe.refund_payment",
  "tool_version": "1.0.0",
  "arguments": {
    "payment_id": "pi_abc123",
    "amount": 150.00
  },
  "context": "Customer requested refund",
  "intent": "Process customer refund request"
}
```

### Response

```json
{
  "decision": "allow",
  "audit_id": "aud_abc123",
  "risk_score": 0.35,
  "pipeline_steps": [
    {"step": "S0", "status": "pass", "duration_ms": 1},
    {"step": "S1", "status": "pass", "duration_ms": 2},
    ...
  ]
}
```

## Tool Definitions

Tools are defined in YAML with schema and risk metadata:

```yaml
apiVersion: invarity.io/v1alpha1
kind: Tool
metadata:
  name: stripe.refund_payment
  version: 1.0.0
  description: Refund a Stripe payment
spec:
  category: payments
  subcategory: refunds
  parameters:
    type: object
    properties:
      payment_id:
        type: string
        description: Stripe payment ID
      amount:
        type: number
        description: Refund amount in dollars
    required:
      - payment_id
      - amount
invarity:
  risk:
    operation: write
    side_effect_scope: external
    resource_scope: single
    base_risk: 0.4
    money_movement: true
    reversibility: difficult
```

## Policies

Policies define rules for tool execution:

```yaml
apiVersion: invarity.io/v1alpha1
kind: Policy
metadata:
  name: refund-limits
  version: 1.0.0
rules:
  - name: small-refunds
    condition: tool.name == "stripe.refund_payment" && arguments.amount < 100
    action: allow

  - name: large-refunds
    condition: tool.name == "stripe.refund_payment" && arguments.amount >= 1000
    action: escalate
    message: "Large refunds require manager approval"

  - name: default
    condition: "true"
    action: allow
```

## CLI Commands

```
invarity
├── ping                    # Health check
├── simulate                # Evaluate tool call
├── tools
│   ├── validate            # Validate tool manifest
│   ├── register            # Register tool with server
│   ├── validate-dir        # Batch validate directory
│   └── register-dir        # Batch register directory
├── toolsets
│   ├── validate            # Validate toolset manifest
│   ├── apply               # Apply toolset to server
│   └── lint                # Lint toolset references
├── policy
│   ├── validate            # Validate policy syntax
│   ├── diff                # Compare local vs server
│   ├── apply               # Upload policy for compilation
│   ├── status              # Check compilation status
│   ├── fuzziness           # View unresolved terms
│   └── promote             # Activate compiled policy
├── audit
│   └── show                # Retrieve audit record
└── version                 # Display version
```

## Configuration

### CLI Configuration

The CLI loads configuration from (in order of precedence):

1. Command-line flags
2. Environment variables
3. `~/.invarity/config.yaml`
4. Defaults

```yaml
# ~/.invarity/config.yaml
server: https://api.invarity.io
org_id: org_abc123
project_id: proj_xyz
env: production
```

Environment variables:
- `INVARITY_SERVER`
- `INVARITY_API_KEY`
- `INVARITY_ORG_ID`
- `INVARITY_ENV`
- `INVARITY_PROJECT_ID`
- `INVARITY_TOOLSET_ID`

### Server Configuration

```bash
# Server settings
PORT=8080
LOG_LEVEL=info

# LLM endpoints
FUNCTIONGEMMA_BASE_URL=http://localhost:8081
FUNCTIONGEMMA_API_KEY=your-key
LLAMAGUARD_BASE_URL=http://localhost:8082
LLAMAGUARD_API_KEY=your-key
QWEN_BASE_URL=http://localhost:8083
QWEN_API_KEY=your-key

# Limits
REQUEST_MAX_BYTES=1048576
MAX_CONTEXT_CHARS=4000
MAX_INTENT_CHARS=1000
CACHE_TTL_SECONDS=300
```

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
- VPC with public/private subnets
- Application Load Balancer
- ECS Fargate service
- DynamoDB tables (6 tables for multi-tenant data)
- S3 buckets (manifests and audit logs)
- KMS encryption
- CloudWatch logging

## Development

### Build

```bash
# CLI
cd invarity-cli && make build

# Server
cd invarity-go && make build

# Infrastructure
cd invarity-infra && npm run build
```

### Test

```bash
# CLI
cd invarity-cli && make test

# Server (with coverage)
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
│   ├── cmd/invarity/          # CLI entry point
│   ├── internal/
│   │   ├── cli/               # Command implementations
│   │   ├── client/            # HTTP client
│   │   ├── config/            # Configuration
│   │   ├── policy/            # Policy validation
│   │   ├── poller/            # Async polling
│   │   └── validate/          # Schema validation
│   └── examples/              # Tool, toolset, policy examples
│
├── invarity-go/
│   ├── cmd/server/            # Server entry point
│   ├── internal/
│   │   ├── audit/             # Audit logging
│   │   ├── config/            # Configuration
│   │   ├── firewall/          # Decision pipeline
│   │   ├── http/              # HTTP handlers
│   │   ├── llm/               # LLM clients
│   │   ├── policy/            # Policy evaluation
│   │   ├── registry/          # Tool registry
│   │   ├── risk/              # Risk computation
│   │   └── types/             # Domain types
│   └── test/                  # Integration tests
│
└── invarity-infra/
    ├── bin/                   # CDK app entry
    ├── lib/                   # Stack definitions
    ├── env/                   # Environment configs
    └── scripts/               # Deployment scripts
```

## License

Proprietary - All rights reserved.
