#!/bin/bash
set -euo pipefail

# Invarity Infrastructure Deploy Script - Dev Environment
# This script deploys the CDK stack to the dev environment

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "=== Invarity Infrastructure Deploy (dev) ==="
echo ""

# Check for required tools
command -v node >/dev/null 2>&1 || { echo "Error: node is required but not installed." >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "Error: npm is required but not installed." >&2; exit 1; }
command -v aws >/dev/null 2>&1 || { echo "Error: aws CLI is required but not installed." >&2; exit 1; }

# Set environment
export INVARITY_ENV=dev
export AWS_REGION=${AWS_REGION:-${AWS_DEFAULT_REGION:-us-east-1}}

# Verify AWS credentials
echo "Checking AWS credentials..."
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text 2>/dev/null) || {
    echo "Error: Unable to get AWS account ID. Please configure AWS credentials."
    exit 1
}

echo "AWS Account: $AWS_ACCOUNT_ID"
echo "AWS Region: $AWS_REGION"
echo "Environment: $INVARITY_ENV"
echo ""

# Install dependencies if needed
if [ ! -d "node_modules" ]; then
    echo "Installing npm dependencies..."
    npm ci
fi

# Show diff first
echo "Running CDK diff..."
echo ""
npx cdk diff --all || true

echo ""
read -p "Do you want to deploy? (y/N) " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo ""
    echo "Deploying CDK stack..."
    npx cdk deploy --all --require-approval never --outputs-file cdk-outputs.json

    echo ""
    echo "=== Deployment Complete ==="
    echo ""
    echo "Stack outputs saved to: cdk-outputs.json"
    echo ""

    # Print key outputs
    if [ -f "cdk-outputs.json" ]; then
        echo "Key outputs:"
        cat cdk-outputs.json | python3 -c "
import json, sys
data = json.load(sys.stdin)
for stack, outputs in data.items():
    print(f'  ALB URL: {outputs.get(\"AlbUrl\", \"N/A\")}')
    print(f'  ECR Repo: {outputs.get(\"EcrRepoUri\", \"N/A\")}')
    break
" 2>/dev/null || echo "  (install python3 to see formatted outputs)"
    fi
else
    echo "Deployment cancelled."
fi
