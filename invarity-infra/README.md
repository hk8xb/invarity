# Invarity Firewall MVP Infrastructure

AWS CDK (TypeScript) infrastructure for the Invarity Firewall MVP - a multi-tenant SaaS platform.

## Architecture Overview

This stack provisions:

- **Networking**: VPC with 2 public + 2 private subnets across 2 AZs, NAT Gateway
- **Load Balancing**: Public ALB on HTTP :80
- **Compute**: ECS Fargate cluster and service
- **Container Registry**: ECR repository for firewall service image
- **Data Storage**: 8 DynamoDB tables with multi-tenant keying, 2 S3 buckets
- **Identity**: Cognito User Pool for human authentication, identity DynamoDB tables
- **Security**: KMS key for encryption, Secrets Manager for API key salt
- **Configuration**: SSM Parameter Store for service discovery

## Multi-Tenancy Design

All data storage is designed for tenant isolation:

### DynamoDB Tables

#### Firewall Tables

| Table | Partition Key | Sort Key | GSIs | Purpose |
|-------|--------------|----------|------|---------|
| `tenants` | `tenant_id` | - | - | Tenant configuration, includes `default_kms_key_arn` for future per-tenant encryption |
| `principals` | `tenant_id` | `principal_id` | - | API principals (agents/services) per tenant, references active `toolset_id#revision` |
| `tools-v2` | `tenant_id` | `tool_version` (`<tool_id>#<version>`) | `tool-versions-index` | Tenant-scoped tool definitions (immutable versions) |
| `toolsets-v2` | `tenant_id` | `toolset_revision` (`<toolset_id>#<revision>`) | `toolset-revisions-index` | Tenant-scoped toolset configurations (immutable revisions) |
| `audit-index` | `tenant_id` | `created_at#audit_id` | `principal-index` | Audit log index |

> **Note**: The `tools-v2` and `toolsets-v2` tables have a `-v2` suffix due to a CloudFormation schema migration. The environment variables and SSM parameters reference the correct table names automatically.

#### Identity Tables

| Table | Partition Key | Sort Key | GSIs | Purpose |
|-------|--------------|----------|------|---------|
| `users` | `user_id` (Cognito sub) | - | `email-index` (email → user_id) | Thin mirror of Cognito users |
| `tenant-memberships` | `tenant_id` | `user_id` | `user-tenants-index` (user_id → tenant_id) | User to tenant role mappings |
| `tokens` | `token_id` | - | `key-hash-index`, `tenant-tokens-index` | Developer and agent runtime tokens |

##### Users Table Schema

| Attribute | Type | Description |
|-----------|------|-------------|
| `user_id` | String (PK) | Cognito `sub` claim |
| `email` | String | User email address |
| `created_at` | String (ISO8601) | Creation timestamp |
| `last_seen_at` | String (ISO8601) | Last activity timestamp |

##### Tenant Memberships Table Schema

| Attribute | Type | Description |
|-----------|------|-------------|
| `tenant_id` | String (PK) | Tenant identifier |
| `user_id` | String (SK) | Cognito `sub` claim |
| `role` | String | `owner` \| `admin` \| `developer` \| `viewer` |
| `status` | String | `active` \| `invited` \| `disabled` |
| `created_at` | String (ISO8601) | Membership creation timestamp |

##### Tokens Table Schema

| Attribute | Type | Description |
|-----------|------|-------------|
| `token_id` | String (PK) | Unique token identifier |
| `token_type` | String | `developer` \| `agent` |
| `tenant_id` | String | Associated tenant |
| `principal_id` | String (optional) | Associated principal for agent tokens |
| `key_hash` | String | SHA-256 hash of the token secret (NEVER store plaintext) |
| `scopes` | List | Permission scopes |
| `principal_allowlist` | List | Allowed principals (for developer tokens) |
| `created_at` | String (ISO8601) | Creation timestamp |
| `created_by_user_id` | String | User who created the token |
| `revoked_at` | String (ISO8601, optional) | Revocation timestamp |
| `last_used_at` | String (ISO8601, optional) | Last usage timestamp |

### S3 Bucket Prefixing Conventions

| Bucket | Prefix Convention | Example Path |
|--------|------------------|--------------|
| `manifests` | `manifests/<tenant_id>/tools/<tool_id>/<version>.json` | `manifests/tenant-123/tools/tool-abc/v1.json` |
| `manifests` | `manifests/<tenant_id>/toolsets/<toolset_id>/<revision>.json` | `manifests/tenant-123/toolsets/toolset-xyz/r3.json` |
| `audit-blobs` | `audit/<tenant_id>/YYYY/MM/DD/<audit_id>.json` | `audit/tenant-123/2024/01/15/audit-xyz.json` |

### Future Enterprise Upgrade Path

The `tenants` table includes a `default_kms_key_arn` field (optional). This supports:
- Per-tenant KMS keys (not implemented in MVP)
- Per-tenant S3 buckets (not implemented in MVP)
- No redesign required when enabling per-tenant encryption

## Cognito Identity

The stack provisions a Cognito User Pool for human user authentication (UI onboarding, later SSO).

### Configuration

Cognito settings are configured in `env/<env>.json`:

```json
{
  "identity": {
    "cognito": {
      "callbackUrls": ["http://localhost:3000/callback"],
      "logoutUrls": ["http://localhost:3000"],
      "domainPrefix": "invarity-dev"
    }
  }
}
```

### Features

- **Email sign-in**: Users authenticate with email address
- **Email verification**: Required for new accounts
- **Password policy**: 8+ chars, upper/lower/digit required
- **OAuth flows**: Authorization Code + PKCE (recommended for SPAs)
- **Hosted UI**: Available at `https://<domainPrefix>.auth.<region>.amazoncognito.com`
- **MFA**: Optional TOTP (can be enabled per-user)

### Integration

Your web application should use the Cognito Hosted UI or AWS Amplify SDK:

```typescript
// Example: Get Cognito config from SSM
const userPoolId = await ssm.getParameter('/invarity/dev/cognito/user_pool_id');
const clientId = await ssm.getParameter('/invarity/dev/cognito/user_pool_client_id');
const issuerUrl = await ssm.getParameter('/invarity/dev/cognito/issuer_url');

// Example: Validate JWT tokens
const jwksUrl = `${issuerUrl}/.well-known/jwks.json`;
```

### OAuth Endpoints

When using the hosted UI domain (`domainPrefix`):

| Endpoint | URL |
|----------|-----|
| Authorize | `https://<domain>.auth.<region>.amazoncognito.com/oauth2/authorize` |
| Token | `https://<domain>.auth.<region>.amazoncognito.com/oauth2/token` |
| UserInfo | `https://<domain>.auth.<region>.amazoncognito.com/oauth2/userInfo` |
| Logout | `https://<domain>.auth.<region>.amazoncognito.com/logout` |

## Prerequisites

- [Node.js](https://nodejs.org/) >= 18.x
- [AWS CLI](https://aws.amazon.com/cli/) configured with credentials
- [Docker](https://www.docker.com/) (for building app images)

## Quick Start

### 1. Bootstrap CDK (First Time Only)

```bash
cd invarity-infra
./scripts/bootstrap.sh
```

### 2. Deploy to Dev

```bash
./scripts/deploy-dev.sh
```

### 3. Push Your First Docker Image

After deployment, push a Docker image to the ECR repository:

```bash
# Get ECR repository URI
ECR_REPO=$(aws ssm get-parameter --name "/invarity/dev/ecr/repo_uri" --query "Parameter.Value" --output text)

# Login to ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $(echo $ECR_REPO | cut -d'/' -f1)

# Build and push (from your app directory)
docker build -t $ECR_REPO:latest .
docker push $ECR_REPO:latest

# Force new deployment
CLUSTER=$(aws ssm get-parameter --name "/invarity/dev/ecs/cluster_name" --query "Parameter.Value" --output text)
SERVICE=$(aws ssm get-parameter --name "/invarity/dev/ecs/service_name" --query "Parameter.Value" --output text)
aws ecs update-service --cluster $CLUSTER --service $SERVICE --force-new-deployment
```

### 4. Access the Service

```bash
ALB_URL=$(aws ssm get-parameter --name "/invarity/dev/alb_url" --query "Parameter.Value" --output text)
curl $ALB_URL/healthz
```

## Environment Variables

The ECS task definition injects these environment variables into your container:

| Variable | Description |
|----------|-------------|
| `INVARITY_ENV` | Environment name (dev, prod) |
| `TENANTS_TABLE` | DynamoDB tenants table name |
| `PRINCIPALS_TABLE` | DynamoDB principals table name |
| `TOOLS_TABLE` | DynamoDB tools table name |
| `TOOLSETS_TABLE` | DynamoDB toolsets table name |
| `AUDIT_INDEX_TABLE` | DynamoDB audit index table name |
| `USERS_TABLE` | DynamoDB users table name |
| `TENANT_MEMBERSHIPS_TABLE` | DynamoDB tenant memberships table name |
| `TOKENS_TABLE` | DynamoDB tokens table name |
| `MANIFESTS_BUCKET` | S3 manifests bucket name |
| `AUDIT_BLOBS_BUCKET` | S3 audit blobs bucket name |
| `KMS_KEY_ARN` | KMS key ARN for encryption |
| `API_KEYS_SALT_SECRET_ARN` | Secrets Manager ARN for API key salt |
| `COGNITO_USER_POOL_ID` | Cognito User Pool ID |
| `COGNITO_USER_POOL_CLIENT_ID` | Cognito User Pool Client ID |
| `COGNITO_ISSUER_URL` | Cognito OIDC Issuer URL |
| `LOG_LEVEL` | Logging level (default: info) |
| `MANIFESTS_PREFIX` | S3 prefix for manifests (`manifests`) |
| `AUDIT_PREFIX` | S3 prefix for audits (`audit`) |

## SSM Parameters

All configuration is stored in SSM Parameter Store under `/invarity/<env>/`:

```
# Core infrastructure
/invarity/dev/alb_url
/invarity/dev/ecr/repo_uri
/invarity/dev/ecs/cluster_name
/invarity/dev/ecs/service_name

# Firewall DynamoDB tables
/invarity/dev/dynamodb/tenants_table
/invarity/dev/dynamodb/principals_table
/invarity/dev/dynamodb/tools_table
/invarity/dev/dynamodb/toolsets_table
/invarity/dev/dynamodb/audit_index_table

# Identity DynamoDB tables
/invarity/dev/dynamodb/users_table
/invarity/dev/dynamodb/tenant_memberships_table
/invarity/dev/dynamodb/tokens_table

# S3 buckets
/invarity/dev/s3/manifests_bucket
/invarity/dev/s3/audit_blobs_bucket
/invarity/dev/s3/manifests_prefix
/invarity/dev/s3/audit_prefix

# Security
/invarity/dev/kms/key_arn
/invarity/dev/secrets/api_keys_salt_arn

# Cognito
/invarity/dev/cognito/user_pool_id
/invarity/dev/cognito/user_pool_arn
/invarity/dev/cognito/user_pool_client_id
/invarity/dev/cognito/issuer_url
/invarity/dev/cognito/hosted_ui_domain  # If domainPrefix is configured
```

## GitHub Actions Setup

### Prerequisites

1. Create an IAM OIDC Identity Provider for GitHub Actions:

```bash
# Create the OIDC provider (one-time setup)
aws iam create-open-id-connect-provider \
    --url https://token.actions.githubusercontent.com \
    --client-id-list sts.amazonaws.com \
    --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1
```

2. Create an IAM Role for GitHub Actions:

```bash
# Create trust policy
cat > trust-policy.json << 'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::ACCOUNT_ID:oidc-provider/token.actions.githubusercontent.com"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
        },
        "StringLike": {
          "token.actions.githubusercontent.com:sub": "repo:YOUR_ORG/YOUR_REPO:*"
        }
      }
    }
  ]
}
EOF

# Replace placeholders
sed -i 's/ACCOUNT_ID/YOUR_AWS_ACCOUNT_ID/g' trust-policy.json
sed -i 's/YOUR_ORG\/YOUR_REPO/your-org\/invarity-infra/g' trust-policy.json

# Create the role
aws iam create-role \
    --role-name invarity-github-actions \
    --assume-role-policy-document file://trust-policy.json

# Attach policies (adjust permissions as needed)
aws iam attach-role-policy \
    --role-name invarity-github-actions \
    --policy-arn arn:aws:iam::aws:policy/AdministratorAccess
```

3. Add the role ARN as a GitHub secret:
   - Go to Repository Settings → Secrets and variables → Actions
   - Add secret: `AWS_ROLE_ARN` = `arn:aws:iam::ACCOUNT_ID:role/invarity-github-actions`

### Workflows

#### Infrastructure Deploy (`infra-deploy-dev.yml`)

Manual workflow to deploy infrastructure changes:

1. Go to Actions → "Infrastructure Deploy (Dev)"
2. Click "Run workflow"
3. Select action: `diff` (preview) or `deploy` (apply)

#### App Deploy (`app-deploy-dev.yml`)

Automatically triggered on push to `main` when app files change:

- Builds Docker image with git SHA tag
- Pushes to ECR
- Forces new ECS deployment
- Waits for service to stabilize

Can also be triggered manually with a custom image tag.

## Directory Structure

```
invarity-infra/
├── bin/
│   └── invarity.ts          # CDK app entry point
├── lib/
│   ├── firewall-stack.ts    # Main infrastructure stack
│   └── identity.ts          # Identity construct (Cognito + DynamoDB)
├── env/
│   └── dev.json             # Dev environment configuration
├── scripts/
│   ├── bootstrap.sh         # CDK bootstrap script
│   ├── deploy-dev.sh        # Dev deployment script
│   └── destroy-dev.sh       # Dev teardown script
├── .github/workflows/
│   ├── infra-deploy-dev.yml # Infrastructure CI/CD
│   └── app-deploy-dev.yml   # Application CI/CD
├── cdk.json                  # CDK configuration
├── package.json              # Node.js dependencies
├── tsconfig.json             # TypeScript configuration
└── README.md                 # This file
```

## Adding a New Environment (e.g., prod)

1. Create `env/prod.json`:

```json
{
  "envName": "prod",
  "project": "invarity",
  "tags": {
    "Project": "invarity",
    "Env": "prod",
    "ManagedBy": "cdk"
  },
  "vpc": {
    "maxAzs": 2,
    "natGateways": 2
  },
  "ecs": {
    "cpu": 512,
    "memoryMiB": 1024,
    "desiredCount": 2,
    "healthCheckPath": "/healthz",
    "containerPort": 8080
  },
  "logs": {
    "retentionDays": 90
  },
  "s3": {
    "manifestsPrefix": "manifests",
    "auditPrefix": "audit"
  },
  "identity": {
    "cognito": {
      "callbackUrls": ["https://app.invarity.com/callback"],
      "logoutUrls": ["https://app.invarity.com"],
      "domainPrefix": "invarity-prod"
    }
  }
}
```

2. Deploy with:

```bash
INVARITY_ENV=prod npx cdk deploy --all
```

## Cleanup

### Destroy Dev Environment

```bash
./scripts/destroy-dev.sh
```

**Note**: Resources with `RETAIN` removal policy will not be deleted:
- DynamoDB tables
- S3 buckets (must be emptied first)
- KMS key

To fully clean up, manually delete these resources in the AWS console.

## Costs (Dev Environment)

Estimated monthly costs for dev environment:

| Resource | Estimated Cost |
|----------|----------------|
| NAT Gateway | ~$32/month + data transfer |
| ALB | ~$16/month + LCU charges |
| Fargate (256 CPU, 512 MB) | ~$10/month |
| DynamoDB (PAY_PER_REQUEST) | Pay per request |
| S3 | Pay per storage/request |
| KMS | ~$1/month per key |
| CloudWatch Logs | Pay per ingestion/storage |
| Cognito | Free tier: 50k MAU, then $0.0055/MAU |

**Total estimate**: ~$60-100/month for dev with minimal traffic.

## Health Check

The ECS task expects a health check endpoint at `/healthz` returning HTTP 200.

Example Go implementation:

```go
http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
})
```

## Troubleshooting

### ECS Service Not Starting

1. Check CloudWatch Logs:
```bash
aws logs tail /ecs/invarity-dev-firewall --follow
```

2. Check ECS events:
```bash
CLUSTER=$(aws ssm get-parameter --name "/invarity/dev/ecs/cluster_name" --query "Parameter.Value" --output text)
SERVICE=$(aws ssm get-parameter --name "/invarity/dev/ecs/service_name" --query "Parameter.Value" --output text)
aws ecs describe-services --cluster $CLUSTER --services $SERVICE --query 'services[0].events[:5]'
```

### Health Check Failing

1. Ensure `/healthz` returns HTTP 200
2. Check security group allows traffic from ALB
3. Verify container is listening on port 8080

### CDK Deployment Fails

1. Ensure CDK is bootstrapped: `./scripts/bootstrap.sh`
2. Check AWS credentials: `aws sts get-caller-identity`
3. Review CDK diff: `npx cdk diff`

## License

Proprietary - Invarity Inc.
