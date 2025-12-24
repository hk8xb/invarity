#!/bin/bash
set -euo pipefail

# Invarity Infrastructure Bootstrap Script
# This script bootstraps the CDK toolkit in your AWS account

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "=== Invarity Infrastructure Bootstrap ==="
echo ""

# Check for required tools
command -v node >/dev/null 2>&1 || { echo "Error: node is required but not installed." >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "Error: npm is required but not installed." >&2; exit 1; }
command -v aws >/dev/null 2>&1 || { echo "Error: aws CLI is required but not installed." >&2; exit 1; }

# Verify AWS credentials
echo "Checking AWS credentials..."
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text 2>/dev/null) || {
    echo "Error: Unable to get AWS account ID. Please configure AWS credentials."
    exit 1
}
AWS_REGION=${AWS_REGION:-${AWS_DEFAULT_REGION:-us-east-1}}

echo "AWS Account: $AWS_ACCOUNT_ID"
echo "AWS Region: $AWS_REGION"
echo ""

# Install dependencies
echo "Installing npm dependencies..."
npm ci

# Bootstrap CDK
echo ""
echo "Bootstrapping CDK in account $AWS_ACCOUNT_ID region $AWS_REGION..."
npx cdk bootstrap aws://$AWS_ACCOUNT_ID/$AWS_REGION \
    --tags Project=invarity \
    --tags ManagedBy=cdk

echo ""
echo "=== Bootstrap Complete ==="
echo ""
echo "Next steps:"
echo "  1. Run: ./scripts/deploy-dev.sh"
echo "  2. Push a Docker image to ECR"
echo "  3. Access the service via ALB URL"
