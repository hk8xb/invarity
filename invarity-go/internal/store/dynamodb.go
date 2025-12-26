package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"

	"invarity/internal/auth"
)

// DynamoDBConfig holds configuration for DynamoDB store.
type DynamoDBConfig struct {
	TenantsTable     string
	UsersTable       string
	MembershipsTable string
	PrincipalsTable  string
	TokensTable      string
	ToolsTable       string
	ToolsetsTable    string
}

// DynamoDBStore implements data access for DynamoDB.
type DynamoDBStore struct {
	client *dynamodb.Client
	config DynamoDBConfig
}

// NewDynamoDBStore creates a new DynamoDB store.
func NewDynamoDBStore(client *dynamodb.Client, cfg DynamoDBConfig) *DynamoDBStore {
	return &DynamoDBStore{
		client: client,
		config: cfg,
	}
}

// --- User Operations ---

// GetOrCreateUser gets an existing user or creates a new one.
func (s *DynamoDBStore) GetOrCreateUser(ctx context.Context, userID, email string) (*User, error) {
	// Try to get existing user
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		// Update last login time
		user.LastLoginAt = time.Now().UTC()
		if err := s.UpdateUserLastLogin(ctx, userID); err != nil {
			// Non-fatal, just log
		}
		return user, nil
	}

	// Create new user
	now := time.Now().UTC()
	newUser := &User{
		UserID:      userID,
		Email:       email,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
		LastLoginAt: now,
	}

	item, err := attributevalue.MarshalMap(newUser)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal user: %w", err)
	}

	// Conditional write to avoid race conditions
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.config.UsersTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(user_id)"),
	})
	if err != nil {
		// If condition failed, user was created by another request
		var condErr *ddbtypes.ConditionalCheckFailedException
		if isConditionCheckFailed(err, condErr) {
			return s.GetUser(ctx, userID)
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return newUser, nil
}

// GetUser retrieves a user by ID.
func (s *DynamoDBStore) GetUser(ctx context.Context, userID string) (*User, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.config.UsersTable),
		Key: map[string]ddbtypes.AttributeValue{
			"user_id": &ddbtypes.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if result.Item == nil {
		return nil, nil
	}

	var user User
	if err := attributevalue.UnmarshalMap(result.Item, &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	return &user, nil
}

// UpdateUserLastLogin updates the last login timestamp.
func (s *DynamoDBStore) UpdateUserLastLogin(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.config.UsersTable),
		Key: map[string]ddbtypes.AttributeValue{
			"user_id": &ddbtypes.AttributeValueMemberS{Value: userID},
		},
		UpdateExpression: aws.String("SET last_login_at = :now, updated_at = :now"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":now": &ddbtypes.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
		},
	})
	return err
}

// --- Tenant Operations ---

// CreateTenant creates a new tenant.
func (s *DynamoDBStore) CreateTenant(ctx context.Context, name, createdBy string) (*Tenant, error) {
	now := time.Now().UTC()
	tenant := &Tenant{
		TenantID:  uuid.New().String(),
		Name:      name,
		Status:    "active",
		Plan:      "free",
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: createdBy,
	}

	item, err := attributevalue.MarshalMap(tenant)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tenant: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.config.TenantsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(tenant_id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	return tenant, nil
}

// GetTenant retrieves a tenant by ID.
func (s *DynamoDBStore) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.config.TenantsTable),
		Key: map[string]ddbtypes.AttributeValue{
			"tenant_id": &ddbtypes.AttributeValueMemberS{Value: tenantID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}
	if result.Item == nil {
		return nil, nil
	}

	var tenant Tenant
	if err := attributevalue.UnmarshalMap(result.Item, &tenant); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tenant: %w", err)
	}
	return &tenant, nil
}

// --- Membership Operations ---

// CreateMembership creates a new tenant membership.
func (s *DynamoDBStore) CreateMembership(ctx context.Context, tenantID, userID string, role auth.Role, invitedBy string) (*TenantMembership, error) {
	now := time.Now().UTC()
	membership := &TenantMembership{
		TenantID:  tenantID,
		UserID:    userID,
		Role:      role,
		Status:    "active",
		InvitedBy: invitedBy,
		CreatedAt: now,
		UpdatedAt: now,
	}

	item, err := attributevalue.MarshalMap(membership)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal membership: %w", err)
	}

	// Use conditional write to avoid duplicates
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.config.MembershipsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(tenant_id) AND attribute_not_exists(user_id)"),
	})
	if err != nil {
		var condErr *ddbtypes.ConditionalCheckFailedException
		if isConditionCheckFailed(err, condErr) {
			return nil, fmt.Errorf("membership already exists")
		}
		return nil, fmt.Errorf("failed to create membership: %w", err)
	}

	return membership, nil
}

// GetMembership retrieves a membership for auth.MembershipChecker interface.
func (s *DynamoDBStore) GetMembership(ctx context.Context, tenantID, userID string) (*auth.Membership, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.config.MembershipsTable),
		Key: map[string]ddbtypes.AttributeValue{
			"tenant_id": &ddbtypes.AttributeValueMemberS{Value: tenantID},
			"user_id":   &ddbtypes.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get membership: %w", err)
	}
	if result.Item == nil {
		return nil, nil
	}

	var membership TenantMembership
	if err := attributevalue.UnmarshalMap(result.Item, &membership); err != nil {
		return nil, fmt.Errorf("failed to unmarshal membership: %w", err)
	}

	return &auth.Membership{
		TenantID: membership.TenantID,
		UserID:   membership.UserID,
		Role:     membership.Role,
		Status:   membership.Status,
	}, nil
}

// ListTenantsForUser lists all tenants a user is a member of.
// Uses GSI: user_id-index with user_id as partition key.
func (s *DynamoDBStore) ListTenantsForUser(ctx context.Context, userID string) ([]TenantWithRole, error) {
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.config.MembershipsTable),
		IndexName:              aws.String("user_id-index"),
		KeyConditionExpression: aws.String("user_id = :uid"),
		FilterExpression:       aws.String("#status = :active"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":uid":    &ddbtypes.AttributeValueMemberS{Value: userID},
			":active": &ddbtypes.AttributeValueMemberS{Value: "active"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query memberships: %w", err)
	}

	var memberships []TenantMembership
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &memberships); err != nil {
		return nil, fmt.Errorf("failed to unmarshal memberships: %w", err)
	}

	// Fetch tenant details for each membership
	tenants := make([]TenantWithRole, 0, len(memberships))
	for _, m := range memberships {
		tenant, err := s.GetTenant(ctx, m.TenantID)
		if err != nil || tenant == nil {
			continue
		}
		tenants = append(tenants, TenantWithRole{
			TenantID: tenant.TenantID,
			Name:     tenant.Name,
			Role:     m.Role,
		})
	}

	return tenants, nil
}

// GetUserOwnedTenant returns the tenant owned by a user (for idempotent bootstrap).
func (s *DynamoDBStore) GetUserOwnedTenant(ctx context.Context, userID string) (*Tenant, error) {
	// Query memberships for user with role=owner
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.config.MembershipsTable),
		IndexName:              aws.String("user_id-index"),
		KeyConditionExpression: aws.String("user_id = :uid"),
		FilterExpression:       aws.String("#role = :owner AND #status = :active"),
		ExpressionAttributeNames: map[string]string{
			"#role":   "role",
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":uid":    &ddbtypes.AttributeValueMemberS{Value: userID},
			":owner":  &ddbtypes.AttributeValueMemberS{Value: string(auth.RoleOwner)},
			":active": &ddbtypes.AttributeValueMemberS{Value: "active"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query owned tenants: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, nil
	}

	var membership TenantMembership
	if err := attributevalue.UnmarshalMap(result.Items[0], &membership); err != nil {
		return nil, fmt.Errorf("failed to unmarshal membership: %w", err)
	}

	return s.GetTenant(ctx, membership.TenantID)
}

// --- Principal Operations ---

// CreatePrincipal creates a new principal under a tenant.
func (s *DynamoDBStore) CreatePrincipal(ctx context.Context, tenantID, name, principalType, createdBy string) (*Principal, error) {
	now := time.Now().UTC()
	principal := &Principal{
		PrincipalID: uuid.New().String(),
		TenantID:    tenantID,
		Name:        name,
		Type:        principalType,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   createdBy,
	}

	item, err := attributevalue.MarshalMap(principal)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal principal: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.config.PrincipalsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(principal_id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create principal: %w", err)
	}

	return principal, nil
}

// GetPrincipal retrieves a principal by ID.
func (s *DynamoDBStore) GetPrincipal(ctx context.Context, principalID string) (*Principal, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.config.PrincipalsTable),
		Key: map[string]ddbtypes.AttributeValue{
			"principal_id": &ddbtypes.AttributeValueMemberS{Value: principalID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get principal: %w", err)
	}
	if result.Item == nil {
		return nil, nil
	}

	var principal Principal
	if err := attributevalue.UnmarshalMap(result.Item, &principal); err != nil {
		return nil, fmt.Errorf("failed to unmarshal principal: %w", err)
	}
	return &principal, nil
}

// ListPrincipals lists principals for a tenant.
func (s *DynamoDBStore) ListPrincipals(ctx context.Context, tenantID string) ([]Principal, error) {
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.config.PrincipalsTable),
		IndexName:              aws.String("tenant_id-index"),
		KeyConditionExpression: aws.String("tenant_id = :tid"),
		FilterExpression:       aws.String("#status = :active"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":tid":    &ddbtypes.AttributeValueMemberS{Value: tenantID},
			":active": &ddbtypes.AttributeValueMemberS{Value: "active"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query principals: %w", err)
	}

	var principals []Principal
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &principals); err != nil {
		return nil, fmt.Errorf("failed to unmarshal principals: %w", err)
	}

	return principals, nil
}

// --- Token Operations (Stubs) ---

// CreateToken creates a new API token.
// Returns the plaintext token (only shown once) and the stored token record.
func (s *DynamoDBStore) CreateToken(ctx context.Context, tenantID, principalID, name, tokenType, createdBy string, scopes []string, expiresAt *time.Time) (string, *Token, error) {
	// Generate token
	tokenID := uuid.New().String()
	plaintext := generateSecureToken()
	keyHash := hashToken(plaintext)
	keyPrefix := plaintext[:8]

	now := time.Now().UTC()
	token := &Token{
		TokenID:     tokenID,
		TenantID:    tenantID,
		PrincipalID: principalID,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		Name:        name,
		Type:        tokenType,
		Scopes:      scopes,
		Status:      "active",
		CreatedAt:   now,
		CreatedBy:   createdBy,
	}
	if expiresAt != nil {
		token.ExpiresAt = *expiresAt
	}

	item, err := attributevalue.MarshalMap(token)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal token: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.config.TokensTable),
		Item:      item,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create token: %w", err)
	}

	return plaintext, token, nil
}

// RevokeToken revokes a token by ID.
func (s *DynamoDBStore) RevokeToken(ctx context.Context, tokenID, revokedBy string) error {
	now := time.Now().UTC()
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.config.TokensTable),
		Key: map[string]ddbtypes.AttributeValue{
			"token_id": &ddbtypes.AttributeValueMemberS{Value: tokenID},
		},
		UpdateExpression: aws.String("SET #status = :revoked, revoked_at = :now, revoked_by = :by"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":revoked": &ddbtypes.AttributeValueMemberS{Value: "revoked"},
			":now":     &ddbtypes.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
			":by":      &ddbtypes.AttributeValueMemberS{Value: revokedBy},
		},
		ConditionExpression: aws.String("attribute_exists(token_id)"),
	})
	return err
}

// ValidateToken validates a token and returns the token record if valid.
func (s *DynamoDBStore) ValidateToken(ctx context.Context, plaintext string) (*Token, error) {
	keyHash := hashToken(plaintext)

	// Query by key_hash using GSI
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.config.TokensTable),
		IndexName:              aws.String("key_hash-index"),
		KeyConditionExpression: aws.String("key_hash = :hash"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":hash": &ddbtypes.AttributeValueMemberS{Value: keyHash},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to validate token: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, nil
	}

	var token Token
	if err := attributevalue.UnmarshalMap(result.Items[0], &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}

	// Check status
	if token.Status != "active" {
		return nil, nil
	}

	// Check expiration
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(time.Now()) {
		return nil, nil
	}

	return &token, nil
}

// --- Tool Operations ---

// ToolRecord represents a tool stored in DynamoDB.
// Uses composite key: tenant_id (PK) + tool_id#version (SK)
type ToolRecord struct {
	TenantID   string `dynamodbav:"tenant_id"`
	SK         string `dynamodbav:"sk"` // tool_id#version
	ToolID     string `dynamodbav:"tool_id"`
	Version    string `dynamodbav:"version"`
	SchemaHash string `dynamodbav:"schema_hash"`
	Name       string `dynamodbav:"name"`
	RiskLevel  string `dynamodbav:"risk_level"`
	S3Key      string `dynamodbav:"s3_key"`
	Status     string `dynamodbav:"status"` // "active", "deprecated"
	CreatedAt  string `dynamodbav:"created_at"`
	UpdatedAt  string `dynamodbav:"updated_at"`
}

// UpsertTool creates or updates a tool metadata record.
// Returns (isNew, error) - isNew is true if this was a new tool version.
func (s *DynamoDBStore) UpsertTool(ctx context.Context, tenantID, toolID, version, schemaHash, name, riskLevel, s3Key string) (bool, error) {
	now := time.Now().UTC()
	sk := toolID + "#" + version

	// First, try to get existing record to check for conflicts
	existing, err := s.GetToolRecord(ctx, tenantID, toolID, version)
	if err != nil {
		return false, err
	}

	if existing != nil {
		// Tool version exists - check schema hash for idempotency
		if existing.SchemaHash == schemaHash {
			// Idempotent - same content, return success
			return false, nil
		}
		// Different hash = conflict
		return false, fmt.Errorf("conflict: tool %s version %s already exists with different schema hash", toolID, version)
	}

	// New tool version - insert
	record := &ToolRecord{
		TenantID:   tenantID,
		SK:         sk,
		ToolID:     toolID,
		Version:    version,
		SchemaHash: schemaHash,
		Name:       name,
		RiskLevel:  riskLevel,
		S3Key:      s3Key,
		Status:     "active",
		CreatedAt:  now.Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	}

	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return false, fmt.Errorf("failed to marshal tool record: %w", err)
	}

	// Conditional put to avoid race conditions
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.config.ToolsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(tenant_id) AND attribute_not_exists(sk)"),
	})
	if err != nil {
		var condErr *ddbtypes.ConditionalCheckFailedException
		if isConditionCheckFailed(err, condErr) {
			// Race condition - another request created it
			return false, fmt.Errorf("conflict: tool version created concurrently")
		}
		return false, fmt.Errorf("failed to create tool record: %w", err)
	}

	return true, nil
}

// GetToolRecord retrieves a tool record by tenant, tool_id, and version.
func (s *DynamoDBStore) GetToolRecord(ctx context.Context, tenantID, toolID, version string) (*ToolRecord, error) {
	sk := toolID + "#" + version

	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.config.ToolsTable),
		Key: map[string]ddbtypes.AttributeValue{
			"tenant_id": &ddbtypes.AttributeValueMemberS{Value: tenantID},
			"sk":        &ddbtypes.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get tool: %w", err)
	}
	if result.Item == nil {
		return nil, nil
	}

	var record ToolRecord
	if err := attributevalue.UnmarshalMap(result.Item, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool: %w", err)
	}
	return &record, nil
}

// ListTools lists all tools for a tenant.
// Returns the latest N tools (optionally with pagination).
func (s *DynamoDBStore) ListTools(ctx context.Context, tenantID string, limit int32) ([]ToolRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.config.ToolsTable),
		KeyConditionExpression: aws.String("tenant_id = :tid"),
		FilterExpression:       aws.String("#status = :active"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":tid":    &ddbtypes.AttributeValueMemberS{Value: tenantID},
			":active": &ddbtypes.AttributeValueMemberS{Value: "active"},
		},
		Limit:            aws.Int32(limit),
		ScanIndexForward: aws.Bool(false), // Newest first
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	var tools []ToolRecord
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &tools); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tools: %w", err)
	}
	return tools, nil
}

// --- Toolset Operations ---

// ToolsetRecord represents a toolset stored in DynamoDB.
// Uses composite key: tenant_id (PK) + toolset_id#revision (SK)
type ToolsetRecord struct {
	TenantID   string `dynamodbav:"tenant_id"`
	SK         string `dynamodbav:"sk"` // toolset_id#revision
	ToolsetID  string `dynamodbav:"toolset_id"`
	Revision   string `dynamodbav:"revision"`
	Name       string `dynamodbav:"name"`
	ToolCount  int    `dynamodbav:"tool_count"`
	S3Key      string `dynamodbav:"s3_key"`
	Status     string `dynamodbav:"status"` // "active", "archived"
	CreatedAt  string `dynamodbav:"created_at"`
	UpdatedAt  string `dynamodbav:"updated_at"`
	CreatedBy  string `dynamodbav:"created_by"`
}

// RegisterToolset creates a new toolset record.
// Returns (isNew, error) - isNew is true if this was a new revision.
func (s *DynamoDBStore) RegisterToolset(ctx context.Context, tenantID, toolsetID, revision, name, s3Key, createdBy string, toolCount int) (bool, error) {
	now := time.Now().UTC()
	sk := toolsetID + "#" + revision

	// Check if already exists
	existing, err := s.GetToolsetRecord(ctx, tenantID, toolsetID, revision)
	if err != nil {
		return false, err
	}
	if existing != nil {
		// Idempotent if same S3 key
		if existing.S3Key == s3Key {
			return false, nil
		}
		return false, fmt.Errorf("conflict: toolset %s revision %s already exists", toolsetID, revision)
	}

	record := &ToolsetRecord{
		TenantID:  tenantID,
		SK:        sk,
		ToolsetID: toolsetID,
		Revision:  revision,
		Name:      name,
		ToolCount: toolCount,
		S3Key:     s3Key,
		Status:    "active",
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		CreatedBy: createdBy,
	}

	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return false, fmt.Errorf("failed to marshal toolset record: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.config.ToolsetsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(tenant_id) AND attribute_not_exists(sk)"),
	})
	if err != nil {
		var condErr *ddbtypes.ConditionalCheckFailedException
		if isConditionCheckFailed(err, condErr) {
			return false, fmt.Errorf("conflict: toolset revision created concurrently")
		}
		return false, fmt.Errorf("failed to create toolset record: %w", err)
	}

	return true, nil
}

// GetToolsetRecord retrieves a toolset record by tenant, toolset_id, and revision.
func (s *DynamoDBStore) GetToolsetRecord(ctx context.Context, tenantID, toolsetID, revision string) (*ToolsetRecord, error) {
	sk := toolsetID + "#" + revision

	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.config.ToolsetsTable),
		Key: map[string]ddbtypes.AttributeValue{
			"tenant_id": &ddbtypes.AttributeValueMemberS{Value: tenantID},
			"sk":        &ddbtypes.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get toolset: %w", err)
	}
	if result.Item == nil {
		return nil, nil
	}

	var record ToolsetRecord
	if err := attributevalue.UnmarshalMap(result.Item, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal toolset: %w", err)
	}
	return &record, nil
}

// ListToolsets lists all toolsets for a tenant.
func (s *DynamoDBStore) ListToolsets(ctx context.Context, tenantID string, limit int32) ([]ToolsetRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.config.ToolsetsTable),
		KeyConditionExpression: aws.String("tenant_id = :tid"),
		FilterExpression:       aws.String("#status = :active"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":tid":    &ddbtypes.AttributeValueMemberS{Value: tenantID},
			":active": &ddbtypes.AttributeValueMemberS{Value: "active"},
		},
		Limit:            aws.Int32(limit),
		ScanIndexForward: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list toolsets: %w", err)
	}

	var toolsets []ToolsetRecord
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &toolsets); err != nil {
		return nil, fmt.Errorf("failed to unmarshal toolsets: %w", err)
	}
	return toolsets, nil
}

// --- Principal Active Toolset Operations ---

// SetPrincipalActiveToolset sets the active toolset for a principal.
func (s *DynamoDBStore) SetPrincipalActiveToolset(ctx context.Context, tenantID, principalID, toolsetID, revision, assignedBy string) error {
	now := time.Now().UTC()

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.config.PrincipalsTable),
		Key: map[string]ddbtypes.AttributeValue{
			"principal_id": &ddbtypes.AttributeValueMemberS{Value: principalID},
		},
		UpdateExpression: aws.String("SET active_toolset_id = :tsid, active_toolset_revision = :rev, toolset_assigned_at = :at, toolset_assigned_by = :by, updated_at = :now"),
		ConditionExpression: aws.String("attribute_exists(principal_id) AND tenant_id = :tid"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":tsid": &ddbtypes.AttributeValueMemberS{Value: toolsetID},
			":rev":  &ddbtypes.AttributeValueMemberS{Value: revision},
			":at":   &ddbtypes.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
			":by":   &ddbtypes.AttributeValueMemberS{Value: assignedBy},
			":now":  &ddbtypes.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
			":tid":  &ddbtypes.AttributeValueMemberS{Value: tenantID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to set principal active toolset: %w", err)
	}

	return nil
}

// GetPrincipalActiveToolset retrieves the active toolset for a principal.
func (s *DynamoDBStore) GetPrincipalActiveToolset(ctx context.Context, principalID string) (toolsetID, revision string, err error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.config.PrincipalsTable),
		Key: map[string]ddbtypes.AttributeValue{
			"principal_id": &ddbtypes.AttributeValueMemberS{Value: principalID},
		},
		ProjectionExpression: aws.String("active_toolset_id, active_toolset_revision"),
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to get principal: %w", err)
	}
	if result.Item == nil {
		return "", "", nil
	}

	if v, ok := result.Item["active_toolset_id"]; ok {
		if s, ok := v.(*ddbtypes.AttributeValueMemberS); ok {
			toolsetID = s.Value
		}
	}
	if v, ok := result.Item["active_toolset_revision"]; ok {
		if s, ok := v.(*ddbtypes.AttributeValueMemberS); ok {
			revision = s.Value
		}
	}

	return toolsetID, revision, nil
}

// --- Helpers ---

func generateSecureToken() string {
	// Generate a secure random token: inv_<uuid without dashes>
	id := uuid.New().String()
	return "inv_" + id[:8] + id[9:13] + id[14:18] + id[19:23] + id[24:]
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func isConditionCheckFailed(err error, _ *ddbtypes.ConditionalCheckFailedException) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*ddbtypes.ConditionalCheckFailedException)
	return ok
}
