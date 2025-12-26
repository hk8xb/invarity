import * as cdk from 'aws-cdk-lib';
import * as cognito from 'aws-cdk-lib/aws-cognito';
import * as dynamodb from 'aws-cdk-lib/aws-dynamodb';
import * as kms from 'aws-cdk-lib/aws-kms';
import * as ssm from 'aws-cdk-lib/aws-ssm';
import { Construct } from 'constructs';

/**
 * Configuration for the Identity Construct
 */
export interface IdentityConstructProps {
  /**
   * Environment name (dev, stage, prod)
   */
  envName: string;

  /**
   * Resource naming prefix (e.g., 'invarity-dev')
   */
  prefix: string;

  /**
   * KMS key for DynamoDB encryption (optional, uses AWS managed if not provided)
   */
  kmsKey?: kms.IKey;

  /**
   * Removal policy for resources (DESTROY for dev, RETAIN for prod)
   */
  removalPolicy: cdk.RemovalPolicy;

  /**
   * Cognito configuration
   */
  cognito: {
    /**
     * Callback URLs for the web app client (e.g., ['http://localhost:3000/callback'])
     */
    callbackUrls: string[];

    /**
     * Logout URLs for the web app client (e.g., ['http://localhost:3000'])
     */
    logoutUrls: string[];

    /**
     * Optional: Domain prefix for Cognito Hosted UI (e.g., 'invarity-dev')
     * If not provided, hosted UI will not be configured
     */
    domainPrefix?: string;
  };
}

/**
 * Identity Construct
 *
 * Provisions Cognito User Pool and DynamoDB tables for the Invarity
 * control-plane identity and tenancy model.
 *
 * Tables:
 * - users: Thin mirror of Cognito users
 * - tenant_memberships: User to tenant role mappings
 * - tokens: Developer and agent runtime tokens (hashed secrets)
 *
 * Note: 'tenants' and 'principals' tables are in FirewallStack
 */
export class IdentityConstruct extends Construct {
  // Cognito resources
  public readonly userPool: cognito.UserPool;
  public readonly userPoolClient: cognito.UserPoolClient;
  public readonly userPoolDomain?: cognito.UserPoolDomain;

  // DynamoDB tables
  public readonly usersTable: dynamodb.Table;
  public readonly tenantMembershipsTable: dynamodb.Table;
  public readonly tokensTable: dynamodb.Table;

  constructor(scope: Construct, id: string, props: IdentityConstructProps) {
    super(scope, id);

    const { envName, prefix, kmsKey, removalPolicy, cognito: cognitoConfig } = props;

    // ========================================
    // Cognito User Pool
    // ========================================
    this.userPool = new cognito.UserPool(this, 'UserPool', {
      userPoolName: `${prefix}-users`,
      selfSignUpEnabled: true,
      signInAliases: {
        email: true,
      },
      autoVerify: {
        email: true,
      },
      standardAttributes: {
        email: {
          required: true,
          mutable: true,
        },
        fullname: {
          required: false,
          mutable: true,
        },
      },
      passwordPolicy: {
        minLength: 8,
        requireLowercase: true,
        requireUppercase: true,
        requireDigits: true,
        requireSymbols: false, // Keep it reasonable for MVP
        tempPasswordValidity: cdk.Duration.days(7),
      },
      accountRecovery: cognito.AccountRecovery.EMAIL_ONLY,
      removalPolicy: removalPolicy,
      // Email configuration - use Cognito default for MVP (SES can be added later)
      email: cognito.UserPoolEmail.withCognito(),
      // MFA - optional for MVP, can be enabled later
      mfa: cognito.Mfa.OPTIONAL,
      mfaSecondFactor: {
        sms: false, // Avoid SMS costs
        otp: true,
      },
    });

    // ========================================
    // Cognito User Pool Client (Web App)
    // ========================================
    this.userPoolClient = new cognito.UserPoolClient(this, 'WebAppClient', {
      userPoolClientName: `${prefix}-web-client`,
      userPool: this.userPool,
      generateSecret: false, // Public client for SPA
      authFlows: {
        userSrp: true,
        userPassword: false, // Prefer SRP for security
      },
      oAuth: {
        flows: {
          authorizationCodeGrant: true, // Authorization Code + PKCE for SPA
          implicitCodeGrant: false, // Deprecated, not recommended
        },
        scopes: [
          cognito.OAuthScope.EMAIL,
          cognito.OAuthScope.OPENID,
          cognito.OAuthScope.PROFILE,
        ],
        callbackUrls: cognitoConfig.callbackUrls,
        logoutUrls: cognitoConfig.logoutUrls,
      },
      preventUserExistenceErrors: true,
      accessTokenValidity: cdk.Duration.hours(1),
      idTokenValidity: cdk.Duration.hours(1),
      refreshTokenValidity: cdk.Duration.days(30),
      supportedIdentityProviders: [
        cognito.UserPoolClientIdentityProvider.COGNITO,
      ],
    });

    // ========================================
    // Cognito Hosted UI Domain (Optional)
    // ========================================
    if (cognitoConfig.domainPrefix) {
      this.userPoolDomain = new cognito.UserPoolDomain(this, 'UserPoolDomain', {
        userPool: this.userPool,
        cognitoDomain: {
          domainPrefix: cognitoConfig.domainPrefix,
        },
      });
    }

    // ========================================
    // DynamoDB: Users Table (Thin Cognito Mirror)
    // ========================================
    // PK: user_id (Cognito 'sub')
    // GSI1: email -> user_id
    this.usersTable = new dynamodb.Table(this, 'UsersTable', {
      tableName: `${prefix}-users`,
      partitionKey: { name: 'user_id', type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: removalPolicy,
      encryption: kmsKey
        ? dynamodb.TableEncryption.CUSTOMER_MANAGED
        : dynamodb.TableEncryption.AWS_MANAGED,
      encryptionKey: kmsKey,
    });

    // GSI: email -> user_id (for lookup by email)
    this.usersTable.addGlobalSecondaryIndex({
      indexName: 'email-index',
      partitionKey: { name: 'email', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'user_id', type: dynamodb.AttributeType.STRING },
      projectionType: dynamodb.ProjectionType.ALL,
    });

    // ========================================
    // DynamoDB: Tenant Memberships Table
    // ========================================
    // PK: tenant_id, SK: user_id
    // GSI1: user_id -> tenant_id (list tenants for a user)
    this.tenantMembershipsTable = new dynamodb.Table(this, 'TenantMembershipsTable', {
      tableName: `${prefix}-tenant-memberships`,
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'user_id', type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: removalPolicy,
      encryption: kmsKey
        ? dynamodb.TableEncryption.CUSTOMER_MANAGED
        : dynamodb.TableEncryption.AWS_MANAGED,
      encryptionKey: kmsKey,
    });

    // GSI: user_id -> tenant_id (list all tenants for a user)
    this.tenantMembershipsTable.addGlobalSecondaryIndex({
      indexName: 'user-tenants-index',
      partitionKey: { name: 'user_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      projectionType: dynamodb.ProjectionType.ALL,
    });

    // ========================================
    // DynamoDB: Tokens Table
    // ========================================
    // PK: token_id
    // GSI1: key_hash -> token_id (for auth lookup)
    // GSI2: tenant_id -> token_id (for listing tokens by tenant)
    this.tokensTable = new dynamodb.Table(this, 'TokensTable', {
      tableName: `${prefix}-tokens`,
      partitionKey: { name: 'token_id', type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecovery: true,
      removalPolicy: removalPolicy,
      encryption: kmsKey
        ? dynamodb.TableEncryption.CUSTOMER_MANAGED
        : dynamodb.TableEncryption.AWS_MANAGED,
      encryptionKey: kmsKey,
    });

    // GSI1: key_hash -> token_id (for authentication lookup by hashed key)
    this.tokensTable.addGlobalSecondaryIndex({
      indexName: 'key-hash-index',
      partitionKey: { name: 'key_hash', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'token_id', type: dynamodb.AttributeType.STRING },
      projectionType: dynamodb.ProjectionType.ALL,
    });

    // GSI2: tenant_id -> token_id (for listing all tokens for a tenant)
    this.tokensTable.addGlobalSecondaryIndex({
      indexName: 'tenant-tokens-index',
      partitionKey: { name: 'tenant_id', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'token_id', type: dynamodb.AttributeType.STRING },
      projectionType: dynamodb.ProjectionType.ALL,
    });

    // ========================================
    // SSM Parameters for service discovery
    // ========================================
    const ssmPrefix = `/invarity/${envName}`;

    // Cognito parameters
    new ssm.StringParameter(this, 'SsmUserPoolId', {
      parameterName: `${ssmPrefix}/cognito/user_pool_id`,
      stringValue: this.userPool.userPoolId,
    });

    new ssm.StringParameter(this, 'SsmUserPoolArn', {
      parameterName: `${ssmPrefix}/cognito/user_pool_arn`,
      stringValue: this.userPool.userPoolArn,
    });

    new ssm.StringParameter(this, 'SsmUserPoolClientId', {
      parameterName: `${ssmPrefix}/cognito/user_pool_client_id`,
      stringValue: this.userPoolClient.userPoolClientId,
    });

    // Issuer URL (OIDC discovery)
    const issuerUrl = `https://cognito-idp.${cdk.Stack.of(this).region}.amazonaws.com/${this.userPool.userPoolId}`;
    new ssm.StringParameter(this, 'SsmCognitoIssuerUrl', {
      parameterName: `${ssmPrefix}/cognito/issuer_url`,
      stringValue: issuerUrl,
    });

    // Hosted UI domain (if configured)
    if (this.userPoolDomain) {
      const hostedUiDomain = `${cognitoConfig.domainPrefix}.auth.${cdk.Stack.of(this).region}.amazoncognito.com`;
      new ssm.StringParameter(this, 'SsmCognitoHostedUiDomain', {
        parameterName: `${ssmPrefix}/cognito/hosted_ui_domain`,
        stringValue: hostedUiDomain,
      });
    }

    // DynamoDB table parameters
    new ssm.StringParameter(this, 'SsmUsersTable', {
      parameterName: `${ssmPrefix}/dynamodb/users_table`,
      stringValue: this.usersTable.tableName,
    });

    new ssm.StringParameter(this, 'SsmTenantMembershipsTable', {
      parameterName: `${ssmPrefix}/dynamodb/tenant_memberships_table`,
      stringValue: this.tenantMembershipsTable.tableName,
    });

    new ssm.StringParameter(this, 'SsmTokensTable', {
      parameterName: `${ssmPrefix}/dynamodb/tokens_table`,
      stringValue: this.tokensTable.tableName,
    });
  }

  /**
   * Get the issuer URL for OIDC discovery
   */
  public getIssuerUrl(): string {
    return `https://cognito-idp.${cdk.Stack.of(this).region}.amazonaws.com/${this.userPool.userPoolId}`;
  }

  /**
   * Get the JWKS URL for token verification
   */
  public getJwksUrl(): string {
    return `${this.getIssuerUrl()}/.well-known/jwks.json`;
  }

  /**
   * Get the OAuth endpoints
   */
  public getOAuthEndpoints(domainPrefix: string, region: string): {
    authorize: string;
    token: string;
    userInfo: string;
    logout: string;
  } {
    const base = `https://${domainPrefix}.auth.${region}.amazoncognito.com`;
    return {
      authorize: `${base}/oauth2/authorize`,
      token: `${base}/oauth2/token`,
      userInfo: `${base}/oauth2/userInfo`,
      logout: `${base}/logout`,
    };
  }
}
