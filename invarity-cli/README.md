# Invarity CLI

Command-line interface for the Invarity control plane - a firewall for agent tool execution.

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/invarity/invarity-cli.git
cd invarity-cli

# Build
go build -o invarity ./cmd/invarity

# Install to PATH (optional)
go install ./cmd/invarity
```

### Build with Version

```bash
go build -ldflags "-X main.version=1.0.0" -o invarity ./cmd/invarity
```

## Configuration

The CLI uses configuration from multiple sources with the following precedence (highest to lowest):

1. **Command-line flags** - `--server`, `--api-key`, `--org`, `--env`
2. **Environment variables** - `INVARITY_SERVER`, `INVARITY_API_KEY`, `INVARITY_ORG_ID`, `INVARITY_ENV`, `INVARITY_TOOLSET_ID`
3. **Config file** - `~/.invarity/config.yaml`
4. **Defaults** - `http://localhost:8080`, env=`sandbox`

### Config File

Create `~/.invarity/config.yaml`:

```yaml
server: https://api.invarity.dev
api_key: your-api-key-here
org_id: org_abc123
env: sandbox          # sandbox, staging, or prod
project_id: proj_xyz  # optional
toolset_id: payments-v1  # optional default toolset
```

### Environment Variables

```bash
export INVARITY_SERVER=https://api.invarity.dev
export INVARITY_API_KEY=your-api-key-here
export INVARITY_ORG_ID=org_abc123
export INVARITY_ENV=sandbox
export INVARITY_PROJECT_ID=proj_xyz  # optional
export INVARITY_TOOLSET_ID=payments-v1  # optional
```

## Commands

### Global Flags

| Flag | Description |
|------|-------------|
| `--server` | Invarity server URL |
| `--api-key` | API key for authentication |
| `--trace` | Print HTTP request/response metadata (for debugging) |
| `--json` | Output raw JSON response (for scripting) |

### `invarity ping`

Check server health status.

```bash
# Basic health check
invarity ping

# With custom server
invarity ping --server https://api.invarity.dev

# JSON output
invarity ping --json
```

### `invarity simulate`

Simulate a tool call evaluation against the firewall.

```bash
# Basic evaluation
invarity simulate -f request.json

# With detailed explanation
invarity simulate -f request.json --explain

# JSON output for scripting
invarity simulate -f request.json --json

# With tracing
invarity simulate -f request.json --trace
```

**Request file format (JSON):**

```json
{
  "tool_name": "stripe.refund_payment",
  "tool_version": "1.0.0",
  "parameters": {
    "payment_id": "pi_abc123",
    "amount": 5000,
    "currency": "USD"
  },
  "context": {
    "session_id": "sess_123",
    "user_id": "user_456"
  }
}
```

### `invarity tools validate`

Validate a tool manifest against the Invarity schema.

```bash
# Validate YAML
invarity tools validate -f tool.yaml

# Validate JSON
invarity tools validate -f tool.json

# JSON output
invarity tools validate -f tool.yaml --json
```

**Exit codes:**
- `0` - Valid manifest
- `1` - Validation errors

### `invarity tools register`

Register a tool with the server.

```bash
# Register a tool
invarity tools register -f tool.yaml --api-key YOUR_KEY

# JSON output
invarity tools register -f tool.json --json
```

The manifest is validated locally before being sent to the server.

### `invarity tools validate-dir`

Validate all tool manifests in a directory recursively.

```bash
# Validate all tools in a directory
invarity tools validate-dir ./tools

# JSON output with per-file results
invarity tools validate-dir ./tools --json
```

Scans for `*.yaml`, `*.yml`, and `*.json` files containing tool definitions.

### `invarity tools register-dir`

Register all tools in a directory with the server.

```bash
# Register all tools in a directory
invarity tools register-dir ./tools

# Continue on errors (don't stop at first failure)
invarity tools register-dir ./tools --continue-on-error

# JSON output
invarity tools register-dir ./tools --json
```

Tools are validated locally before registration. Up to 4 tools are registered concurrently for performance.

---

## Toolset Management

Toolsets group related tools together with environment bindings and optional policy references.

### `invarity toolsets validate`

Validate a toolset manifest against the Invarity Toolset Schema.

```bash
# Validate a toolset
invarity toolsets validate -f toolset.yaml

# JSON output
invarity toolsets validate -f toolset.json --json
```

### `invarity toolsets apply`

Apply a toolset to the server.

```bash
# Apply a toolset
invarity toolsets apply -f toolset.yaml

# Override environment and status
invarity toolsets apply -f toolset.yaml --env prod --status ACTIVE

# JSON output
invarity toolsets apply -f toolset.yaml --json
```

### `invarity toolsets lint`

Lint a toolset against a tools directory to verify all tool references exist.

```bash
# Lint toolset against tools directory
invarity toolsets lint -f toolset.yaml --tools-dir ./tools

# JSON output
invarity toolsets lint -f toolset.yaml --tools-dir ./tools --json
```

**Lint checks:**
- All tool references (`id@version`) exist in the tools directory
- Reports missing tools (errors)
- Reports unreferenced tools (warnings)
- Reports invalid tool files that couldn't be parsed
- Reports tools missing `invarity.id` or `invarity.version`

---

## Policy Management

The Invarity CLI provides a complete policy lifecycle workflow:

```
validate → diff → apply → status → fuzziness → promote
```

### Policy Lifecycle Overview

1. **validate** - Check policy syntax locally (no server required)
2. **diff** - Compare local policy against active policy on server
3. **apply** - Upload policy for compilation
4. **status** - Check compilation progress
5. **fuzziness** - Review unresolved terms and required variables
6. **promote** - Activate the compiled policy

### Policy Command Flags

All policy commands support these additional flags:

| Flag | Description |
|------|-------------|
| `--org` | Organization ID (overrides config) |
| `--env` | Environment: sandbox, staging, prod |
| `--project` | Project ID (optional) |

### `invarity policy validate`

Validate a policy file locally without contacting the server.

```bash
# Validate a policy
invarity policy validate -f policy.yaml

# JSON output with validation report
invarity policy validate -f policy.yaml --json
```

**Output includes:**
- Syntax validation
- Required field checks (apiVersion, kind, metadata, spec)
- Rule structure validation
- Warnings for potential issues
- Canonical preview of the policy

### `invarity policy diff`

Compare a local policy with the currently active policy on the server.

```bash
# Basic diff
invarity policy diff -f policy.yaml

# Diff against specific org/env
invarity policy diff -f policy.yaml --org my-org --env prod

# JSON output
invarity policy diff -f policy.yaml --json
```

If the server doesn't support fetching active policies yet, the command shows a canonical rendering of the local policy instead.

### `invarity policy apply`

Upload a policy to the server for compilation.

```bash
# Apply a policy
invarity policy apply -f policy.yaml --org my-org

# Apply and wait for compilation
invarity policy apply -f policy.yaml --wait

# Apply to production
invarity policy apply -f policy.yaml --org my-org --env prod

# JSON output
invarity policy apply -f policy.yaml --json
```

**Response includes:**
- `policy_version` - Unique version ID for this policy
- `status` - COMPILING, READY, or FAILED
- `fuzziness_report` - If the policy has unresolved terms

**With `--wait`:**
- Polls server with exponential backoff
- Shows progress indicator
- Returns when compilation completes or fails
- Maximum wait time: 5 minutes

### `invarity policy status`

Check the compilation status of a policy version.

```bash
# Check status
invarity policy status pol_abc123

# JSON output
invarity policy status pol_abc123 --json
```

**Status values:**
- `PENDING` - Queued for compilation
- `COMPILING` - Currently being processed
- `READY` - Successfully compiled, ready for activation
- `FAILED` - Compilation failed (check errors)

### `invarity policy fuzziness`

View the fuzziness report for a policy version.

```bash
# View fuzziness report
invarity policy fuzziness pol_abc123

# JSON output
invarity policy fuzziness pol_abc123 --json
```

**Report includes:**
- **Unresolved Terms** - Terms that couldn't be mapped to known concepts
- **Required Variables** - Variables referenced but not defined
- **Suggested Mappings** - Potential resolutions with confidence scores
- **Fuzziness Score** - Overall ambiguity level (0-1)
- **What This Means** - Explanation of runtime behavior

**When fuzziness exists:**
Requests matching fuzzy conditions may **ESCALATE** to human review until terms are resolved or variables are provided.

### `invarity policy promote`

Activate a compiled policy version.

```bash
# Promote to active
invarity policy promote pol_abc123 --active

# JSON output
invarity policy promote pol_abc123 --active --json
```

Only policies with `READY` status can be promoted. Once promoted, the policy becomes active for the org/environment.

---

### Policy Workflow Examples

#### Complete Deployment Workflow

```bash
# 1. Validate locally
invarity policy validate -f policy.yaml

# 2. Check diff against current active policy
invarity policy diff -f policy.yaml --org my-org --env prod

# 3. Apply and wait for compilation
invarity policy apply -f policy.yaml --org my-org --env prod --wait

# 4. Review fuzziness (if any)
invarity policy fuzziness pol_abc123

# 5. Promote to active
invarity policy promote pol_abc123 --active
```

#### CI/CD Integration

```bash
#!/bin/bash
set -e

# Validate all policies
for f in policies/*.yaml; do
  invarity policy validate -f "$f"
done

# Apply to staging
RESULT=$(invarity policy apply -f policies/main.yaml \
  --org "$ORG_ID" --env staging --wait --json)

VERSION=$(echo "$RESULT" | jq -r '.policy_version')
STATUS=$(echo "$RESULT" | jq -r '.status')

if [ "$STATUS" != "READY" ]; then
  echo "Policy compilation failed"
  exit 1
fi

# Check fuzziness score
FUZZ=$(invarity policy fuzziness "$VERSION" --json | jq -r '.fuzziness_score // 0')
if (( $(echo "$FUZZ > 0.5" | bc -l) )); then
  echo "Warning: High fuzziness score ($FUZZ)"
  # Optionally fail or require manual review
fi

# Promote if ready
invarity policy promote "$VERSION" --active
echo "Policy $VERSION activated"
```

#### Handling Fuzzy Policies

```bash
# Apply policy with fuzzy terms
invarity policy apply -f policy.fuzzy.yaml --org my-org

# Output:
# ✓ Policy applied successfully
#   Policy Version:  pol_xyz789
#   Status:          READY
#
# Fuzziness Report
#   Score: 0.65
#   Unresolved Terms: 3
#   Required Variables: 2

# Get detailed fuzziness report
invarity policy fuzziness pol_xyz789

# Output shows what needs resolution before the policy
# can make deterministic decisions
```

---

### `invarity audit show`

Retrieve an audit record by ID.

```bash
# Show audit record
invarity audit show abc123

# JSON output
invarity audit show abc123 --json
```

### `invarity version`

Display version information.

```bash
invarity version
invarity version --json
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Validation error (invalid input, schema violation) |
| `2` | Network/server error (connection failed, server error) |

## Tool Manifest Schema

Tool manifests follow the conventional tool format (compatible with OpenAI/Claude) with an additional `invarity` block for firewall metadata. See [examples/tool.stripe.refund.yaml](examples/tool.stripe.refund.yaml) for a complete example.

### Required Fields

```yaml
name: my_tool_name          # Provider-facing name (snake_case, <=64 chars)
description: What the tool does

# Either parameters (OpenAI-style) or input_schema (Claude-style)
parameters:
  type: object
  additionalProperties: false
  properties:
    # parameter definitions
  required:
    - required_param

# Invarity-specific metadata
invarity:
  id: my.tool.id           # Unique identifier (3-128 chars)
  version: 1.0.0           # Semantic version
  risk:
    operation: write       # read, write, delete, execute
    side_effect_scope: external  # none, internal, external, global
    resource_scope: single       # single, scoped_collection, global
    # Optional but recommended:
    base_risk: high        # low, medium, high, critical
    data_class: financial  # public, internal, confidential, pii, phi, financial
    money_movement: true
    reversibility: partially_reversible  # reversible, partially_reversible, irreversible
```

### Risk Metadata

The `invarity.risk` block provides deterministic risk metadata for the firewall:

| Field | Required | Values |
|-------|----------|--------|
| `operation` | Yes | `read`, `write`, `delete`, `execute` |
| `side_effect_scope` | Yes | `none`, `internal`, `external`, `global` |
| `resource_scope` | Yes | `single`, `scoped_collection`, `global` |
| `base_risk` | No | `low`, `medium`, `high`, `critical` |
| `data_class` | No | `public`, `internal`, `confidential`, `pii`, `phi`, `financial` |
| `money_movement` | No | `true`, `false` |
| `reversibility` | No | `reversible`, `partially_reversible`, `irreversible` |
| `bulk` | No | `true`, `false` |
| `max_bulk` | No | `2` - `1000000` (only if bulk=true) |
| `requires_human_review` | No | `true`, `false` |

## Toolset Schema

Toolsets group related tools together with environment bindings. See [examples/toolset.payments.yaml](examples/toolset.payments.yaml) for a complete example.

### Required Fields

```yaml
toolset_id: my-toolset-v1   # Unique identifier (1-128 chars)

tools:                       # Tool references (1-5000 items)
  - id: stripe.refund_payment
    version: 1.0.0
  - id: database.query
    version: 1.0.0

# Optional fields
envs:                        # Environments where toolset is available
  - sandbox
  - prod
status: ACTIVE               # DRAFT, ACTIVE, DEPRECATED
description: What this toolset is for
policy:
  bundle_id: policy-bundle-id
  version: 1.0.0
labels:
  team: payments
  owner: team@example.com
```

## Policy Schema

Policies define rules for controlling tool execution. See [examples/policy.simple.yaml](examples/policy.simple.yaml) for a complete example.

### Required Fields

```yaml
apiVersion: invarity.dev/v1
kind: Policy
metadata:
  name: my-policy
  version: 1.0.0
spec:
  rules:
    - name: rule-name
      condition: |
        tool.category == "financial"
      action: allow  # allow, deny, escalate, review
  defaultAction: allow
```

### Policy Actions

- `allow` - Permit the tool call
- `deny` - Block the tool call
- `escalate` - Require human approval
- `review` - Flag for review but allow

## Examples

### Evaluate a Refund Request

```bash
# Create request file
cat > request.json << 'EOF'
{
  "tool_name": "stripe.refund_payment",
  "parameters": {
    "payment_id": "pi_abc123",
    "amount": 15000,
    "currency": "USD",
    "reason": "customer_request"
  },
  "context": {
    "user_id": "user_123",
    "session_id": "sess_456"
  }
}
EOF

# Evaluate
invarity simulate -f request.json --explain
```

### Validate and Register a Tool

```bash
# Validate first
invarity tools validate -f tool.yaml

# If valid, register
invarity tools register -f tool.yaml
```

### Scripting with JSON Output

```bash
# Get decision as JSON for processing
RESULT=$(invarity simulate -f request.json --json)
DECISION=$(echo "$RESULT" | jq -r '.decision')

if [ "$DECISION" = "deny" ]; then
  echo "Tool call blocked"
  exit 1
fi
```

### CI/CD Validation

```bash
# Validate all tool manifests in a directory
for f in tools/*.yaml; do
  if ! invarity tools validate -f "$f"; then
    echo "Invalid: $f"
    exit 1
  fi
done
echo "All manifests valid"
```

## Development

### Project Structure

```
invarity-cli/
├── cmd/invarity/          # Main entry point
│   └── main.go
├── internal/
│   ├── cli/               # CLI commands (cobra)
│   │   ├── root.go
│   │   ├── ping.go
│   │   ├── simulate.go
│   │   ├── tools.go
│   │   ├── toolsets.go
│   │   ├── policy.go
│   │   ├── audit.go
│   │   └── version.go
│   ├── client/            # HTTP client
│   │   └── client.go
│   ├── config/            # Configuration loading
│   │   └── config.go
│   ├── policy/            # Policy validation & rendering
│   │   ├── validate.go
│   │   └── canonical.go
│   ├── poller/            # Async polling utilities
│   │   └── poller.go
│   └── validate/          # JSON Schema validation
│       ├── validate.go
│       └── schemas/
│           ├── invarity.tool.schema.json
│           └── invarity.toolset.schema.json
├── schemas/               # Public schema files
│   ├── invarity.tool.schema.json
│   └── invarity.toolset.schema.json
├── examples/              # Example files
│   ├── request.refund_escalate.json
│   ├── tool.stripe.refund.yaml
│   ├── tool.database.query.yaml
│   ├── toolset.payments.yaml
│   ├── toolset.support.yaml
│   ├── policy.default.yaml
│   ├── policy.simple.yaml
│   └── policy.fuzzy.yaml
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### Building

```bash
# Development build
go build ./cmd/invarity

# Release build with version
VERSION=1.0.0
go build -ldflags "-X main.version=$VERSION" -o invarity ./cmd/invarity

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o invarity-linux-amd64 ./cmd/invarity
GOOS=darwin GOARCH=arm64 go build -o invarity-darwin-arm64 ./cmd/invarity
GOOS=windows GOARCH=amd64 go build -o invarity-windows-amd64.exe ./cmd/invarity
```

### Testing

```bash
go test ./...
```

## License

Copyright © Invarity
