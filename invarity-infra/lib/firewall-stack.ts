import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as ecr from 'aws-cdk-lib/aws-ecr';
import * as elbv2 from 'aws-cdk-lib/aws-elasticloadbalancingv2';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as dynamodb from 'aws-cdk-lib/aws-dynamodb';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as kms from 'aws-cdk-lib/aws-kms';
import * as secretsmanager from 'aws-cdk-lib/aws-secretsmanager';
import * as ssm from 'aws-cdk-lib/aws-ssm';
import * as iam from 'aws-cdk-lib/aws-iam';
import { Construct } from 'constructs';
import { IdentityConstruct } from './identity';

export interface FirewallStackProps extends cdk.StackProps {
  envName: string;
  envConfig: {
    project: string;
    tags: Record<string, string>;
    vpc: {
      maxAzs: number;
      natGateways: number;
    };
    ecs: {
      cpu: number;
      memoryMiB: number;
      desiredCount: number;
      healthCheckPath: string;
      containerPort: number;
    };
    logs: {
      retentionDays: number;
    };
    s3: {
      manifestsPrefix: string;
      auditPrefix: string;
    };
    identity?: {
      cognito: {
        callbackUrls: string[];
        logoutUrls: string[];
        domainPrefix?: string;
      };
    };
  };
}

export class FirewallStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props: FirewallStackProps) {
    super(scope, id, props);

    const { envName, envConfig } = props;
    const prefix = `invarity-${envName}`;

    // ========================================
    // KMS Key for encryption
    // ========================================
    const kmsKey = new kms.Key(this, 'InvarityKey', {
      alias: `alias/invarity/${envName}`,
      description: `Invarity ${envName} encryption key`,
      enableKeyRotation: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ========================================
    // VPC with public and private subnets
    // ========================================
    const vpc = new ec2.Vpc(this, 'Vpc', {
      vpcName: `${prefix}-vpc`,
      maxAzs: envConfig.vpc.maxAzs,
      natGateways: envConfig.vpc.natGateways,
      subnetConfiguration: [
        {
          name: 'public',
          subnetType: ec2.SubnetType.PUBLIC,
          cidrMask: 24,
        },
        {
          name: 'private',
          subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS,
          cidrMask: 24,
        },
      ],
    });

    // ========================================
    // Security Groups
    // ========================================
    const albSecurityGroup = new ec2.SecurityGroup(this, 'AlbSecurityGroup', {
      vpc,
      securityGroupName: `${prefix}-alb-sg`,
      description: 'Security group for ALB',
      allowAllOutbound: true,
    });
    albSecurityGroup.addIngressRule(
      ec2.Peer.anyIpv4(),
      ec2.Port.tcp(80),
      'Allow HTTP traffic'
    );

    const ecsSecurityGroup = new ec2.SecurityGroup(this, 'EcsSecurityGroup', {
      vpc,
      securityGroupName: `${prefix}-ecs-sg`,
      description: 'Security group for ECS service',
      allowAllOutbound: true,
    });
    ecsSecurityGroup.addIngressRule(
      albSecurityGroup,
      ec2.Port.tcp(envConfig.ecs.containerPort),
      'Allow traffic from ALB'
    );

    // ========================================
    // Application Load Balancer
    // ========================================
    const alb = new elbv2.ApplicationLoadBalancer(this, 'Alb', {
      vpc,
      loadBalancerName: `${prefix}-alb`,
      internetFacing: true,
      securityGroup: albSecurityGroup,
      vpcSubnets: { subnetType: ec2.SubnetType.PUBLIC },
    });

    // ========================================
    // ECR Repository
    // ========================================
    const ecrRepo = new ecr.Repository(this, 'EcrRepo', {
      repositoryName: `${prefix}-firewall`,
      removalPolicy: cdk.RemovalPolicy.DESTROY,
      emptyOnDelete: true,
      imageScanOnPush: true,
      lifecycleRules: [
        {
          maxImageCount: 10,
          rulePriority: 1,
          description: 'Keep only 10 images',
        },
      ],
    });

    // ========================================
    // ECS Cluster
    // ========================================
    const cluster = new ecs.Cluster(this, 'Cluster', {
      clusterName: `${prefix}-cluster`,
      vpc,
      containerInsights: true,
    });

    // ========================================
    // CloudWatch Logs Group
    // ========================================
    const logGroup = new logs.LogGroup(this, 'LogGroup', {
      logGroupName: `/ecs/${prefix}-firewall`,
      retention: envConfig.logs.retentionDays,
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });

    // ========================================
    // Secrets Manager - API Keys Salt
    // ========================================
    const apiKeysSaltSecret = new secretsmanager.Secret(this, 'ApiKeysSaltSecret', {
      secretName: `${prefix}/firewall-api-keys-salt`,
      description: 'Salt for hashing API keys',
      generateSecretString: {
        passwordLength: 32,
        excludePunctuation: true,
      },
    });

    // ========================================
    // DynamoDB Tables (Multi-tenant keying)
    // ========================================

    // Tenants Table - PK: tenant_id
    const tenantsTable = new dynamodb.Table(this, 'TenantsTable', {
      tableName: `${prefix}-tenants`,
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
      encryption: dynamodb.TableEncryption.CUSTOMER_MANAGED,
      encryptionKey: kmsKey,
    });

    // Principals Table - PK: tenant_id, SK: principal_id
    const principalsTable = new dynamodb.Table(this, 'PrincipalsTable', {
      tableName: `${prefix}-principals`,
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'principal_id', type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
      encryption: dynamodb.TableEncryption.CUSTOMER_MANAGED,
      encryptionKey: kmsKey,
    });

    // Tools Table - PK: tenant_id, SK: tool_id#version (tenant-scoped registry)
    // Attributes: schema_hash, name, description, created_at, s3_key
    // S3 path: manifests/{tenant}/tools/{tool_id}/{version}.json
    const toolsTable = new dynamodb.Table(this, 'ToolsTableV2', {
      tableName: `${prefix}-tools-v2`,
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'tool_version', type: dynamodb.AttributeType.STRING }, // tool_id#version
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
      encryption: dynamodb.TableEncryption.CUSTOMER_MANAGED,
      encryptionKey: kmsKey,
    });

    // GSI for listing versions of a specific tool
    toolsTable.addGlobalSecondaryIndex({
      indexName: 'tool-versions-index',
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'tool_id', type: dynamodb.AttributeType.STRING },
      projectionType: dynamodb.ProjectionType.ALL,
    });

    // Toolsets Table - PK: tenant_id, SK: toolset_id#revision (tenant-scoped registry)
    // Attributes: created_at, s3_key, tool_refs (list of {tool_id, version}), metadata
    // S3 path: manifests/{tenant}/toolsets/{toolset_id}/{revision}.json
    const toolsetsTable = new dynamodb.Table(this, 'ToolsetsTableV2', {
      tableName: `${prefix}-toolsets-v2`,
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'toolset_revision', type: dynamodb.AttributeType.STRING }, // toolset_id#revision
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
      encryption: dynamodb.TableEncryption.CUSTOMER_MANAGED,
      encryptionKey: kmsKey,
    });

    // GSI for listing revisions of a specific toolset
    toolsetsTable.addGlobalSecondaryIndex({
      indexName: 'toolset-revisions-index',
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'toolset_id', type: dynamodb.AttributeType.STRING },
      projectionType: dynamodb.ProjectionType.ALL,
    });

    // Audit Index Table - PK: tenant_id, SK: created_at#audit_id
    const auditIndexTable = new dynamodb.Table(this, 'AuditIndexTable', {
      tableName: `${prefix}-audit-index`,
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'created_audit', type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
      encryption: dynamodb.TableEncryption.CUSTOMER_MANAGED,
      encryptionKey: kmsKey,
    });

    // GSI for per-principal queries
    auditIndexTable.addGlobalSecondaryIndex({
      indexName: 'principal-index',
      partitionKey: { name: 'tenant_principal', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'created_audit', type: dynamodb.AttributeType.STRING },
      projectionType: dynamodb.ProjectionType.ALL,
    });

    // ========================================
    // Identity Construct (Cognito + User/Membership/Token tables)
    // ========================================
    // Determine removal policy based on environment
    const identityRemovalPolicy = envName === 'dev'
      ? cdk.RemovalPolicy.DESTROY
      : cdk.RemovalPolicy.RETAIN;

    // Default cognito config for backwards compatibility
    const defaultCognitoConfig = {
      callbackUrls: ['http://localhost:3000/callback'],
      logoutUrls: ['http://localhost:3000'],
      domainPrefix: `invarity-${envName}`,
    };

    const identityConfig = envConfig.identity?.cognito || defaultCognitoConfig;

    const identity = new IdentityConstruct(this, 'Identity', {
      envName,
      prefix,
      kmsKey,
      removalPolicy: identityRemovalPolicy,
      cognito: identityConfig,
    });

    // ========================================
    // S3 Buckets (Versioned, encrypted)
    // ========================================

    // Manifests Bucket
    const manifestsBucket = new s3.Bucket(this, 'ManifestsBucket', {
      bucketName: `${prefix}-manifests-${this.account}`,
      versioned: true,
      encryption: s3.BucketEncryption.KMS,
      encryptionKey: kmsKey,
      blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
      enforceSSL: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // Audit Blobs Bucket
    const auditBlobsBucket = new s3.Bucket(this, 'AuditBlobsBucket', {
      bucketName: `${prefix}-audit-blobs-${this.account}`,
      versioned: true,
      encryption: s3.BucketEncryption.KMS,
      encryptionKey: kmsKey,
      blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
      enforceSSL: true,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
      lifecycleRules: [
        {
          id: 'transition-to-ia',
          enabled: true,
          transitions: [
            {
              storageClass: s3.StorageClass.INFREQUENT_ACCESS,
              transitionAfter: cdk.Duration.days(90),
            },
          ],
        },
      ],
    });

    // ========================================
    // ECS Task Definition
    // ========================================
    const taskRole = new iam.Role(this, 'TaskRole', {
      roleName: `${prefix}-firewall-task-role`,
      assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com'),
    });

    // Grant permissions to task role
    tenantsTable.grantReadWriteData(taskRole);
    principalsTable.grantReadWriteData(taskRole);
    toolsTable.grantReadWriteData(taskRole);
    toolsetsTable.grantReadWriteData(taskRole);
    auditIndexTable.grantReadWriteData(taskRole);
    manifestsBucket.grantReadWrite(taskRole);
    auditBlobsBucket.grantReadWrite(taskRole);
    kmsKey.grantEncryptDecrypt(taskRole);
    apiKeysSaltSecret.grantRead(taskRole);

    // Grant permissions to identity tables
    identity.usersTable.grantReadWriteData(taskRole);
    identity.tenantMembershipsTable.grantReadWriteData(taskRole);
    identity.tokensTable.grantReadWriteData(taskRole);

    const taskDefinition = new ecs.FargateTaskDefinition(this, 'TaskDef', {
      family: `${prefix}-firewall`,
      cpu: envConfig.ecs.cpu,
      memoryLimitMiB: envConfig.ecs.memoryMiB,
      taskRole,
    });

    const container = taskDefinition.addContainer('firewall', {
      image: ecs.ContainerImage.fromEcrRepository(ecrRepo, 'latest'),
      logging: ecs.LogDrivers.awsLogs({
        streamPrefix: 'firewall',
        logGroup,
      }),
      environment: {
        INVARITY_ENV: envName,
        TENANTS_TABLE: tenantsTable.tableName,
        PRINCIPALS_TABLE: principalsTable.tableName,
        TOOLS_TABLE: toolsTable.tableName,
        TOOLSETS_TABLE: toolsetsTable.tableName,
        AUDIT_INDEX_TABLE: auditIndexTable.tableName,
        MANIFESTS_BUCKET: manifestsBucket.bucketName,
        AUDIT_BLOBS_BUCKET: auditBlobsBucket.bucketName,
        KMS_KEY_ARN: kmsKey.keyArn,
        API_KEYS_SALT_SECRET_ARN: apiKeysSaltSecret.secretArn,
        LOG_LEVEL: 'info',
        // S3 prefix conventions for app to use
        MANIFESTS_PREFIX: envConfig.s3.manifestsPrefix,
        AUDIT_PREFIX: envConfig.s3.auditPrefix,
        // Identity tables
        USERS_TABLE: identity.usersTable.tableName,
        TENANT_MEMBERSHIPS_TABLE: identity.tenantMembershipsTable.tableName,
        TOKENS_TABLE: identity.tokensTable.tableName,
        // Cognito
        COGNITO_USER_POOL_ID: identity.userPool.userPoolId,
        COGNITO_USER_POOL_CLIENT_ID: identity.userPoolClient.userPoolClientId,
        COGNITO_ISSUER_URL: identity.getIssuerUrl(),
      },
      healthCheck: {
        command: ['CMD-SHELL', `curl -f http://localhost:${envConfig.ecs.containerPort}/healthz || exit 1`],
        interval: cdk.Duration.seconds(30),
        timeout: cdk.Duration.seconds(5),
        retries: 3,
        startPeriod: cdk.Duration.seconds(60),
      },
    });

    container.addPortMappings({
      containerPort: envConfig.ecs.containerPort,
      protocol: ecs.Protocol.TCP,
    });

    // ========================================
    // ECS Service
    // ========================================
    const service = new ecs.FargateService(this, 'Service', {
      serviceName: `${prefix}-firewall`,
      cluster,
      taskDefinition,
      desiredCount: envConfig.ecs.desiredCount,
      securityGroups: [ecsSecurityGroup],
      vpcSubnets: { subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS },
      assignPublicIp: false,
      circuitBreaker: { rollback: true },
      enableExecuteCommand: true,
    });

    // ========================================
    // ALB Target Group and Listener
    // ========================================
    const targetGroup = new elbv2.ApplicationTargetGroup(this, 'TargetGroup', {
      targetGroupName: `${prefix}-firewall-tg`,
      vpc,
      port: envConfig.ecs.containerPort,
      protocol: elbv2.ApplicationProtocol.HTTP,
      targetType: elbv2.TargetType.IP,
      healthCheck: {
        path: envConfig.ecs.healthCheckPath,
        interval: cdk.Duration.seconds(30),
        timeout: cdk.Duration.seconds(5),
        healthyThresholdCount: 2,
        unhealthyThresholdCount: 3,
        healthyHttpCodes: '200',
      },
    });

    service.attachToApplicationTargetGroup(targetGroup);

    alb.addListener('HttpListener', {
      port: 80,
      protocol: elbv2.ApplicationProtocol.HTTP,
      defaultTargetGroups: [targetGroup],
    });

    // ========================================
    // SSM Parameters for service discovery
    // ========================================
    const ssmPrefix = `/invarity/${envName}`;

    new ssm.StringParameter(this, 'SsmAlbUrl', {
      parameterName: `${ssmPrefix}/alb_url`,
      stringValue: `http://${alb.loadBalancerDnsName}`,
    });

    new ssm.StringParameter(this, 'SsmEcrRepoUri', {
      parameterName: `${ssmPrefix}/ecr/repo_uri`,
      stringValue: ecrRepo.repositoryUri,
    });

    new ssm.StringParameter(this, 'SsmClusterName', {
      parameterName: `${ssmPrefix}/ecs/cluster_name`,
      stringValue: cluster.clusterName,
    });

    new ssm.StringParameter(this, 'SsmServiceName', {
      parameterName: `${ssmPrefix}/ecs/service_name`,
      stringValue: service.serviceName,
    });

    new ssm.StringParameter(this, 'SsmTenantsTable', {
      parameterName: `${ssmPrefix}/dynamodb/tenants_table`,
      stringValue: tenantsTable.tableName,
    });

    new ssm.StringParameter(this, 'SsmPrincipalsTable', {
      parameterName: `${ssmPrefix}/dynamodb/principals_table`,
      stringValue: principalsTable.tableName,
    });

    new ssm.StringParameter(this, 'SsmToolsTable', {
      parameterName: `${ssmPrefix}/dynamodb/tools_table`,
      stringValue: toolsTable.tableName,
    });

    new ssm.StringParameter(this, 'SsmToolsetsTable', {
      parameterName: `${ssmPrefix}/dynamodb/toolsets_table`,
      stringValue: toolsetsTable.tableName,
    });

    new ssm.StringParameter(this, 'SsmAuditIndexTable', {
      parameterName: `${ssmPrefix}/dynamodb/audit_index_table`,
      stringValue: auditIndexTable.tableName,
    });

    new ssm.StringParameter(this, 'SsmManifestsBucket', {
      parameterName: `${ssmPrefix}/s3/manifests_bucket`,
      stringValue: manifestsBucket.bucketName,
    });

    new ssm.StringParameter(this, 'SsmAuditBlobsBucket', {
      parameterName: `${ssmPrefix}/s3/audit_blobs_bucket`,
      stringValue: auditBlobsBucket.bucketName,
    });

    new ssm.StringParameter(this, 'SsmKmsKeyArn', {
      parameterName: `${ssmPrefix}/kms/key_arn`,
      stringValue: kmsKey.keyArn,
    });

    new ssm.StringParameter(this, 'SsmApiKeysSaltSecretArn', {
      parameterName: `${ssmPrefix}/secrets/api_keys_salt_arn`,
      stringValue: apiKeysSaltSecret.secretArn,
    });

    // S3 prefix conventions as constants
    new ssm.StringParameter(this, 'SsmManifestsPrefix', {
      parameterName: `${ssmPrefix}/s3/manifests_prefix`,
      stringValue: envConfig.s3.manifestsPrefix,
    });

    new ssm.StringParameter(this, 'SsmAuditPrefix', {
      parameterName: `${ssmPrefix}/s3/audit_prefix`,
      stringValue: envConfig.s3.auditPrefix,
    });

    // ========================================
    // Stack Outputs
    // ========================================
    new cdk.CfnOutput(this, 'AlbUrl', {
      value: `http://${alb.loadBalancerDnsName}`,
      description: 'Application Load Balancer URL',
      exportName: `${prefix}-alb-url`,
    });

    new cdk.CfnOutput(this, 'EcrRepoUri', {
      value: ecrRepo.repositoryUri,
      description: 'ECR Repository URI',
      exportName: `${prefix}-ecr-repo-uri`,
    });

    new cdk.CfnOutput(this, 'ClusterName', {
      value: cluster.clusterName,
      description: 'ECS Cluster Name',
      exportName: `${prefix}-cluster-name`,
    });

    new cdk.CfnOutput(this, 'ServiceName', {
      value: service.serviceName,
      description: 'ECS Service Name',
      exportName: `${prefix}-service-name`,
    });

    new cdk.CfnOutput(this, 'TenantsTableName', {
      value: tenantsTable.tableName,
      description: 'Tenants DynamoDB Table',
      exportName: `${prefix}-tenants-table`,
    });

    new cdk.CfnOutput(this, 'PrincipalsTableName', {
      value: principalsTable.tableName,
      description: 'Principals DynamoDB Table',
      exportName: `${prefix}-principals-table`,
    });

    new cdk.CfnOutput(this, 'ToolsTableName', {
      value: toolsTable.tableName,
      description: 'Tools DynamoDB Table',
      exportName: `${prefix}-tools-table`,
    });

    new cdk.CfnOutput(this, 'ToolsetsTableName', {
      value: toolsetsTable.tableName,
      description: 'Toolsets DynamoDB Table',
      exportName: `${prefix}-toolsets-table`,
    });

    new cdk.CfnOutput(this, 'AuditIndexTableName', {
      value: auditIndexTable.tableName,
      description: 'Audit Index DynamoDB Table',
      exportName: `${prefix}-audit-index-table`,
    });

    new cdk.CfnOutput(this, 'ManifestsBucketName', {
      value: manifestsBucket.bucketName,
      description: 'Manifests S3 Bucket',
      exportName: `${prefix}-manifests-bucket`,
    });

    new cdk.CfnOutput(this, 'AuditBlobsBucketName', {
      value: auditBlobsBucket.bucketName,
      description: 'Audit Blobs S3 Bucket',
      exportName: `${prefix}-audit-blobs-bucket`,
    });

    new cdk.CfnOutput(this, 'KmsKeyArn', {
      value: kmsKey.keyArn,
      description: 'KMS Key ARN',
      exportName: `${prefix}-kms-key-arn`,
    });

    new cdk.CfnOutput(this, 'ApiKeysSaltSecretArn', {
      value: apiKeysSaltSecret.secretArn,
      description: 'API Keys Salt Secret ARN',
      exportName: `${prefix}-api-keys-salt-secret-arn`,
    });

    // ========================================
    // Identity Outputs
    // ========================================
    new cdk.CfnOutput(this, 'CognitoUserPoolId', {
      value: identity.userPool.userPoolId,
      description: 'Cognito User Pool ID',
      exportName: `${prefix}-cognito-user-pool-id`,
    });

    new cdk.CfnOutput(this, 'CognitoUserPoolArn', {
      value: identity.userPool.userPoolArn,
      description: 'Cognito User Pool ARN',
      exportName: `${prefix}-cognito-user-pool-arn`,
    });

    new cdk.CfnOutput(this, 'CognitoUserPoolClientId', {
      value: identity.userPoolClient.userPoolClientId,
      description: 'Cognito User Pool Client ID (Web App)',
      exportName: `${prefix}-cognito-user-pool-client-id`,
    });

    new cdk.CfnOutput(this, 'CognitoIssuerUrl', {
      value: identity.getIssuerUrl(),
      description: 'Cognito OIDC Issuer URL',
      exportName: `${prefix}-cognito-issuer-url`,
    });

    if (identity.userPoolDomain) {
      new cdk.CfnOutput(this, 'CognitoHostedUiDomain', {
        value: `${identityConfig.domainPrefix}.auth.${this.region}.amazoncognito.com`,
        description: 'Cognito Hosted UI Domain',
        exportName: `${prefix}-cognito-hosted-ui-domain`,
      });
    }

    new cdk.CfnOutput(this, 'UsersTableName', {
      value: identity.usersTable.tableName,
      description: 'Users DynamoDB Table',
      exportName: `${prefix}-users-table`,
    });

    new cdk.CfnOutput(this, 'TenantMembershipsTableName', {
      value: identity.tenantMembershipsTable.tableName,
      description: 'Tenant Memberships DynamoDB Table',
      exportName: `${prefix}-tenant-memberships-table`,
    });

    new cdk.CfnOutput(this, 'TokensTableName', {
      value: identity.tokensTable.tableName,
      description: 'Tokens DynamoDB Table',
      exportName: `${prefix}-tokens-table`,
    });
  }
}
