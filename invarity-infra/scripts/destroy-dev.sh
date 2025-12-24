#!/bin/bash
set -euo pipefail

# Invarity Infrastructure Destroy Script - Dev Environment
# This script destroys the CDK stack in the dev environment

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "=== Invarity Infrastructure Destroy (dev) ==="
echo ""
echo "WARNING: This will destroy all resources in the dev environment!"
echo "Note: Some resources (DynamoDB tables, S3 buckets, KMS keys) have RETAIN policy"
echo "      and will need to be manually deleted if desired."
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

read -p "Are you sure you want to destroy the dev environment? (type 'destroy' to confirm) " -r
echo ""

if [[ $REPLY == "destroy" ]]; then
    echo ""
    echo "Destroying CDK stack..."
    npx cdk destroy --all --force

    echo ""
    echo "=== Destroy Complete ==="
    echo ""
    echo "Note: Resources with RETAIN policy may still exist:"
    echo "  - DynamoDB tables (invarity-dev-*)"
    echo "  - S3 buckets (invarity-dev-*)"
    echo "  - KMS key (alias/invarity/dev)"
    echo ""
    echo "To fully clean up, manually delete these resources in the AWS console."
else
    echo "Destroy cancelled. You must type 'destroy' exactly to confirm."
fi
