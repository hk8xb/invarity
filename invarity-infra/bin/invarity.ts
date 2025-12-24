#!/usr/bin/env node
import 'source-map-support/register';
import * as cdk from 'aws-cdk-lib';
import * as fs from 'fs';
import * as path from 'path';
import { FirewallStack } from '../lib/firewall-stack';

// Load environment configuration
const envName = process.env.INVARITY_ENV || 'dev';
const envConfigPath = path.join(__dirname, '..', 'env', `${envName}.json`);

if (!fs.existsSync(envConfigPath)) {
  throw new Error(`Environment config not found: ${envConfigPath}`);
}

const envConfig = JSON.parse(fs.readFileSync(envConfigPath, 'utf-8'));

const app = new cdk.App();

// Create the Firewall Stack
const firewallStack = new FirewallStack(app, `invarity-${envName}-firewall`, {
  env: {
    account: process.env.CDK_DEFAULT_ACCOUNT,
    region: process.env.CDK_DEFAULT_REGION || 'us-east-1',
  },
  envName: envConfig.envName,
  envConfig: envConfig,
  description: `Invarity Firewall MVP Infrastructure - ${envName}`,
});

// Apply tags from config
Object.entries(envConfig.tags as Record<string, string>).forEach(([key, value]) => {
  cdk.Tags.of(firewallStack).add(key, value);
});

app.synth();
