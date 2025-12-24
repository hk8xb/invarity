# Invarity Firewall MVP Infrastructure

AWS CDK (TypeScript) infrastructure for the Invarity Firewall MVP - a multi-tenant SaaS platform.

## Architecture Overview

This stack provisions:

- **Networking**: VPC with 2 public + 2 private subnets across 2 AZs, NAT Gateway
- **Load Balancing**: Public ALB on HTTP :80
- **Compute**: ECS Fargate cluster and service
- **Container Registry**: ECR repository for firewall service image
- **Data Storage**: 6 DynamoDB tables with multi-tenant keying, 2 S3 buckets
- **Security**: KMS key for encryption, Secrets Manager for API key salt
- **Configuration**: SSM Parameter Store for service discovery

## Multi-Tenancy Design

All data storage is designed for tenant isolation:

### DynamoDB Tables

| Table | Partition Key | Sort Key | Purpose |
|-------|--------------|----------|---------|
| `tenants` | `tenant_id` | - | Tenant configuration, includes `default_kms_key_arn` for future per-tenant encryption |
| `principals` | `tenant_id` | `principal_id` | API principals (users/services) per tenant |
| `tools` | `tenant_id#principal_id` | `tool_key` (`<tool_id>#<version>`) | Tool definitions |
| `toolsets` | `tenant_id#principal_id` | `toolset_id` | Toolset configurations |
| `toolset-revisions` | `tenant_id#principal_id` | `<toolset_id>#<revision>` | Toolset version history |
| `audit-index` | `tenant_id` | `created_at#audit_id` | Audit log index (GSI for per-principal queries) |

### S3 Bucket Prefixing Conventions

| Bucket | Prefix Convention | Example Path |
|--------|------------------|--------------|
| `manifests` | `manifests/<tenant_id>/...` | `manifests/tenant-123/tools/tool-abc.json` |
| `audit-blobs` | `audit/<tenant_id>/YYYY/MM/DD/<audit_id>.json` | `audit/tenant-123/2024/01/15/audit-xyz.json` |

### Future Enterprise Upgrade Path

The `tenants` table includes a `default_kms_key_arn` field (optional). This supports:
- Per-tenant KMS keys (not implemented in MVP)
- Per-tenant S3 buckets (not implemented in MVP)
- No redesign required when enabling per-tenant encryption

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
| `TOOLSET_REVISIONS_TABLE` | DynamoDB toolset revisions table name |
| `AUDIT_INDEX_TABLE` | DynamoDB audit index table name |
| `MANIFESTS_BUCKET` | S3 manifests bucket name |
| `AUDIT_BLOBS_BUCKET` | S3 audit blobs bucket name |
| `KMS_KEY_ARN` | KMS key ARN for encryption |
| `API_KEYS_SALT_SECRET_ARN` | Secrets Manager ARN for API key salt |
| `LOG_LEVEL` | Logging level (default: info) |
| `MANIFESTS_PREFIX` | S3 prefix for manifests (`manifests`) |
| `AUDIT_PREFIX` | S3 prefix for audits (`audit`) |

## SSM Parameters

All configuration is stored in SSM Parameter Store under `/invarity/<env>/`:

```
/invarity/dev/alb_url
/invarity/dev/ecr/repo_uri
/invarity/dev/ecs/cluster_name
/invarity/dev/ecs/service_name
/invarity/dev/dynamodb/tenants_table
/invarity/dev/dynamodb/principals_table
/invarity/dev/dynamodb/tools_table
/invarity/dev/dynamodb/toolsets_table
/invarity/dev/dynamodb/toolset_revisions_table
/invarity/dev/dynamodb/audit_index_table
/invarity/dev/s3/manifests_bucket
/invarity/dev/s3/audit_blobs_bucket
/invarity/dev/s3/manifests_prefix
/invarity/dev/s3/audit_prefix
/invarity/dev/kms/key_arn
/invarity/dev/secrets/api_keys_salt_arn
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
│   └── firewall-stack.ts    # Main infrastructure stack
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
