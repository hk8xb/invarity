# Invarity CLI

Command-line interface for the Invarity control plane - a firewall for agent tool execution.

## Concepts

The Invarity CLI follows a clear ontology:

- **Tools** are registered to a **tenant's tool library** (not to principals)
- **Toolsets** bundle tool references and are registered to a **tenant's toolset library**
- **Toolsets are applied to principals** to grant access to specific tools

This separation allows tools to be defined once and reused across multiple toolsets and principals.

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

1. **Command-line flags** - `--server`, `--api-key`, `--tenant`, `--principal`
2. **Environment variables** - `INVARITY_SERVER`, `INVARITY_API_KEY`, `INVARITY_TENANT_ID`, `INVARITY_PRINCIPAL_ID`
3. **Config file** - `~/.invarity/config.yaml`
4. **Defaults** - `http://localhost:8080`, tenant=`default`

### Config File

Create `~/.invarity/config.yaml`:

```yaml
server: https://api.invarity.dev
api_key: your-api-key-here
tenant_id: acme              # Default tenant for tool/toolset registration
principal_id: my-agent       # Default principal for applying toolsets
```

### Environment Variables

```bash
export INVARITY_SERVER=https://api.invarity.dev
export INVARITY_API_KEY=your-api-key-here
export INVARITY_TENANT_ID=acme
export INVARITY_PRINCIPAL_ID=my-agent
```

## Recommended Workflow

The typical workflow for setting up an agent with Invarity:

```bash
# 1. Set up configuration
export INVARITY_TENANT_ID=acme  # optional, defaults to "default"

# 2. Register tools to the tenant's library
invarity tools register-dir ./tools

# 3. Register a toolset to the tenant's library
invarity toolsets register -f toolset.yaml --tools-dir ./tools

# 4. Apply the toolset to a principal
invarity principals apply-toolset --principal my-agent --toolset payments-v1 --revision 1.0.0
```

## Commands

### Global Flags

| Flag | Description |
|------|-------------|
| `--server` | Invarity server URL |
| `--api-key` | API key for authentication |
| `--tenant` | Tenant ID for operations |
| `--principal` | Principal ID (for principal operations) |
| `--trace` | Print HTTP request/response metadata (for debugging) |
| `--json` | Output raw JSON response (for scripting) |

### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Validation error (invalid input, schema violation) |
| `2` | Network/server error (connection failed, server error) |

---

## Tool Registration

Tools are registered to a **tenant's tool library**. Once registered, tools can be referenced by toolsets.

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

### `invarity tools register`

Register a tool with the tenant's tool library.

```bash
# Register a tool
invarity tools register -f tool.yaml

# Register with explicit tenant
invarity tools register -f tool.yaml --tenant acme

# Read from stdin
cat tool.json | invarity tools register --stdin

# JSON output
invarity tools register -f tool.json --json
```

**What happens during registration:**
1. Validates the tool manifest against the embedded JSON schema
2. Normalizes enum values to lowercase (operation, base_risk, etc.)
3. Computes `schema_hash` if not present: `sha256:<hex of canonicalized invarity block>`
4. POSTs to `/v1/tenants/{tenant}/tools`

**Response includes:**
- `tool_id` - Unique identifier for the registered tool
- `version` - Semantic version
- `schema_hash` - SHA256 hash of the tool schema

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

Register all tools in a directory with the tenant's library.

```bash
# Register all tools in a directory
invarity tools register-dir ./tools

# With explicit tenant
invarity tools register-dir ./tools --tenant acme

# Continue on validation errors (register valid tools only)
invarity tools register-dir ./tools --continue-on-error

# JSON output
invarity tools register-dir ./tools --json
```

**Behavior:**
- Validates all files locally first
- By default, aborts if any file fails validation (no partial registration)
- Use `--continue-on-error` to register valid tools and report failures
- Up to 4 tools are registered concurrently for performance

---

## Toolset Management

Toolsets bundle tool references and are registered to a **tenant's toolset library**. Tools referenced by a toolset must exist in the tenant's tool registry.

### `invarity toolsets validate`

Validate a toolset manifest against the Invarity Toolset Schema.

```bash
# Validate a toolset
invarity toolsets validate -f toolset.yaml

# JSON output
invarity toolsets validate -f toolset.json --json
```

### `invarity toolsets register`

Register a toolset with the tenant's toolset library.

```bash
# Register a toolset
invarity toolsets register -f toolset.yaml

# Register with auto-registration of referenced tools
invarity toolsets register -f toolset.yaml --tools-dir ./tools

# With explicit tenant
invarity toolsets register -f toolset.yaml --tenant acme

# JSON output
invarity toolsets register -f toolset.yaml --json
```

**When `--tools-dir` is provided:**
1. Verifies all tools referenced in the toolset exist in the directory
2. Automatically registers the referenced tools before registering the toolset
3. Only registers tools that are referenced by the toolset (not all tools in the directory)

### `invarity toolsets lint`

Lint a toolset against a tools directory to verify all tool references exist.

```bash
# Lint toolset against tools directory
invarity toolsets lint -f toolset.yaml --tools-dir ./tools

# JSON output
invarity toolsets lint -f toolset.yaml --tools-dir ./tools --json
```

**Lint checks:**
- All tool references (`tool_id@version`) exist in the tools directory
- Reports missing tools (errors)
- Reports unreferenced tools (warnings)
- Reports invalid tool files that couldn't be parsed
- Reports tools missing `invarity.id` or `invarity.version`

---

## Principal Management

Principals are the agents or services that use toolsets. Toolsets are applied to principals to grant access to specific tools.

### `invarity principals apply-toolset`

Apply a registered toolset to a principal.

```bash
# Apply a toolset to a principal
invarity principals apply-toolset --principal my-agent --toolset payments-v1 --revision 1.0.0

# With explicit tenant
invarity principals apply-toolset --tenant acme --principal my-agent --toolset payments-v1 --revision 1.0.0

# JSON output
invarity principals apply-toolset --principal my-agent --toolset payments-v1 --revision 1.0.0 --json
```

**Required flags:**
- `--principal` - Principal ID to apply the toolset to
- `--toolset` - Toolset ID to apply
- `--revision` - Toolset revision to apply

---

## Utility Commands

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

---

## Tool Manifest Schema (v3)

Tool manifests follow the conventional tool format (compatible with OpenAI/Claude) with an additional `invarity` block for firewall metadata. The schema uses a **deterministic constraints model** - all constraints must be explicitly defined, no inference.

### Required Fields

```yaml
name: my_tool_name          # Provider-facing name (snake_case, <=64 chars)
description: What the tool does

# Either parameters (OpenAI-style) or input_schema (Claude-style)
parameters:
  type: object
  additionalProperties: false
  properties:
    amount:
      type: number
    recipient_id:
      type: string
  required:
    - amount
    - recipient_id

# Invarity-specific metadata
invarity:
  id: my.tool.id           # Opaque tool identifier (1-256 chars)
  version: 1.0.0           # Semantic version
  # schema_hash is computed automatically if not provided
  # schema_hash: sha256:<hex>

  # Risk metadata (all fields required)
  risk:
    base_risk: high              # low, medium, high, critical
    operation: write             # read, write, delete, execute
    requires_human_review: false
    tags: ["financial", "pii"]   # Optional customer labels
    notes: "Handles payment refunds"  # Optional notes

  # Constraints (all fields required - no inference)
  constraints:
    requires_justification: true
    required_args: ["amount", "recipient_id"]
    disallow_wildcards: true
    max_bulk: null               # null or integer (1-1000000)
    amount_limit:                # null or object
      max: 10000
      currency: USD
      arg_key: amount            # Must exist in parameters/input_schema properties
    notes: "Max refund $100"     # Optional notes (max 512 chars)

  # Optional: Hard size limits for LLM context budgets
  limits:
    max_description_chars: 800
    max_constraints_notes_chars: 256
```

### Schema Hash Computation

The `schema_hash` field provides a content-addressable identifier for the tool schema. If not provided, the CLI computes it automatically as:

```
sha256:<hex of canonicalized JSON of the invarity block>
```

The canonical form has sorted keys and no extra whitespace.

### Risk Metadata

The `invarity.risk` block provides deterministic risk metadata for the firewall.

| Field | Required | Values | Description |
|-------|----------|--------|-------------|
| `base_risk` | Yes | `low`, `medium`, `high`, `critical` | Base risk level of the tool |
| `operation` | Yes | `read`, `write`, `delete`, `execute` | Type of operation the tool performs |
| `requires_human_review` | Yes | `true`, `false` | Whether this tool always requires human review |
| `tags` | No | string[] | Customer labels for routing/reporting (max 32 items) |
| `notes` | No | string | Optional notes (max 2000 chars) |

### Constraints

The `invarity.constraints` block defines deterministic constraints for tool execution. **Required fields must be present - no inference.**

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `requires_justification` | Yes | boolean | Whether a justification string is required |
| `required_args` | Yes | string[] | Argument keys that must be provided (max 32 items) |
| `disallow_wildcards` | Yes | boolean | Reject wildcard patterns like `*`, `ALL`, empty filters |
| `max_bulk` | Yes | integer/null | Maximum bulk size (1-1000000), or null if not applicable |
| `amount_limit` | Yes | object/null | Money movement limit, or null if not applicable |
| `notes` | No | string | Optional notes for humans (max 512 chars) |

### Limits (Optional)

The `invarity.limits` block defines hard size limits to protect LLM context budgets.

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `max_description_chars` | Yes (if limits present) | integer | Max description length (100-2000, default 800) |
| `max_constraints_notes_chars` | Yes (if limits present) | integer | Max constraints.notes length (0-512, default 256) |

### Constraint Lint Checks

The CLI performs additional lint checks beyond schema validation:

1. **Amount limit arg**: If `amount_limit` is set, `amount_limit.arg_key` must exist in tool parameters
2. **Description length**: If `limits` is set, description length must not exceed `max_description_chars`
3. **Notes length**: If `limits` is set, `constraints.notes` length must not exceed `max_constraints_notes_chars`

## Toolset Schema

Toolsets bundle tool references and are registered to a tenant. They can then be applied to principals. See [examples/toolset.payments.yaml](examples/toolset.payments.yaml) for a complete example.

### Required Fields

```yaml
toolset_id: my-toolset-v1   # Unique identifier (1-128 chars)
revision: "1.0.0"           # Immutable revision identifier

tools:                       # Tool references (1-500 items)
  - tool_id: stripe.refund_payment
    version: 1.0.0
  - tool_id: database.query
    version: 1.0.0

# Optional fields
display_name: My Toolset     # Human-friendly name
description: What this toolset is for
labels:
  team: payments
  owner: team@example.com
```

## Examples

### Complete Registration Workflow

```bash
# 1. Validate tools locally
invarity tools validate-dir ./tools

# 2. Register all tools to the tenant's library
invarity tools register-dir ./tools

# 3. Lint toolset against tools
invarity toolsets lint -f toolset.yaml --tools-dir ./tools

# 4. Register toolset to the tenant's library
invarity toolsets register -f toolset.yaml

# 5. Apply toolset to a principal
invarity principals apply-toolset --principal my-agent --toolset payments-v1 --revision 1.0.0
```

### One-Step Toolset Registration

```bash
# Register tools and toolset in one command
invarity toolsets register -f toolset.yaml --tools-dir ./tools
```

### Scripting with JSON Output

```bash
# Get registration result as JSON
RESULT=$(invarity tools register -f tool.yaml --json)
TOOL_ID=$(echo "$RESULT" | jq -r '.tool_id')
SCHEMA_HASH=$(echo "$RESULT" | jq -r '.schema_hash')

echo "Registered tool: $TOOL_ID with hash: $SCHEMA_HASH"
```

### CI/CD Validation

```bash
#!/bin/bash
set -e

# Validate all tool manifests in a directory
invarity tools validate-dir ./tools

# Validate toolset
invarity toolsets validate -f toolset.yaml

# Lint toolset against tools
invarity toolsets lint -f toolset.yaml --tools-dir ./tools

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
│   │   ├── principals.go
│   │   ├── audit.go
│   │   └── version.go
│   ├── client/            # HTTP client
│   │   └── client.go
│   ├── config/            # Configuration loading
│   │   └── config.go
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
│   └── toolset.support.yaml
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
