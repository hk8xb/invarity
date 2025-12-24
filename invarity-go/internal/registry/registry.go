// Package registry provides tool registry interfaces and implementations.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"invarity/internal/types"
)

// ErrToolNotFound is returned when a tool is not found in the registry.
var ErrToolNotFound = fmt.Errorf("tool not found")

// ErrVersionMismatch is returned when the tool version/schema hash doesn't match.
var ErrVersionMismatch = fmt.Errorf("version/schema hash mismatch")

// Store defines the interface for tool registry storage.
type Store interface {
	// GetTool retrieves a tool by action_id and version.
	// If version is empty, returns the latest version.
	GetTool(ctx context.Context, actionID, version string) (*types.ToolRegistryEntry, error)

	// GetToolBySchemaHash retrieves a tool by action_id and schema hash.
	GetToolBySchemaHash(ctx context.Context, actionID, schemaHash string) (*types.ToolRegistryEntry, error)

	// ListTools returns all tools (for admin/debug purposes).
	ListTools(ctx context.Context) ([]*types.ToolRegistryEntry, error)

	// PutTool adds or updates a tool in the registry.
	PutTool(ctx context.Context, entry *types.ToolRegistryEntry) error

	// DeleteTool removes a tool from the registry.
	DeleteTool(ctx context.Context, actionID, version string) error
}

// InMemoryStore is an in-memory implementation of Store for local development.
type InMemoryStore struct {
	mu    sync.RWMutex
	tools map[string]*types.ToolRegistryEntry // key: actionID:version
}

// NewInMemoryStore creates a new in-memory registry store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		tools: make(map[string]*types.ToolRegistryEntry),
	}
}

// NewInMemoryStoreWithDefaults creates a store pre-populated with sample tools.
func NewInMemoryStoreWithDefaults() *InMemoryStore {
	store := NewInMemoryStore()

	// Add sample tools for development/testing
	sampleTools := []*types.ToolRegistryEntry{
		{
			ActionID:    "send_email",
			Version:     "1.0.0",
			SchemaHash:  "abc123",
			Name:        "Send Email",
			Description: "Sends an email to specified recipients",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to": {"type": "array", "items": {"type": "string", "format": "email"}},
					"subject": {"type": "string", "maxLength": 200},
					"body": {"type": "string", "maxLength": 50000},
					"cc": {"type": "array", "items": {"type": "string", "format": "email"}},
					"attachments": {"type": "array", "items": {"type": "string"}}
				},
				"required": ["to", "subject", "body"],
				"additionalProperties": false
			}`),
			RiskProfile: types.RiskProfile{
				MoneyMovement:   false,
				PrivilegeChange: false,
				Irreversible:    true,
				BulkOperation:   false,
				ResourceScope:   "single",
				DataClass:       "internal",
			},
		},
		{
			ActionID:    "transfer_funds",
			Version:     "1.0.0",
			SchemaHash:  "def456",
			Name:        "Transfer Funds",
			Description: "Transfers money between accounts",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"from_account": {"type": "string"},
					"to_account": {"type": "string"},
					"amount": {"type": "number", "minimum": 0.01},
					"currency": {"type": "string", "enum": ["USD", "EUR", "GBP"]},
					"memo": {"type": "string", "maxLength": 500}
				},
				"required": ["from_account", "to_account", "amount", "currency"],
				"additionalProperties": false
			}`),
			RiskProfile: types.RiskProfile{
				MoneyMovement:    true,
				PrivilegeChange:  false,
				Irreversible:     true,
				BulkOperation:    false,
				ResourceScope:    "single",
				DataClass:        "confidential",
				RequiresApproval: true,
			},
		},
		{
			ActionID:    "delete_user",
			Version:     "1.0.0",
			SchemaHash:  "ghi789",
			Name:        "Delete User",
			Description: "Permanently deletes a user account",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"user_id": {"type": "string"},
					"reason": {"type": "string"},
					"cascade": {"type": "boolean", "default": false}
				},
				"required": ["user_id", "reason"],
				"additionalProperties": false
			}`),
			RiskProfile: types.RiskProfile{
				MoneyMovement:    false,
				PrivilegeChange:  true,
				Irreversible:     true,
				BulkOperation:    false,
				ResourceScope:    "single",
				DataClass:        "restricted",
				RequiresApproval: true,
			},
		},
		{
			ActionID:    "bulk_update_records",
			Version:     "1.0.0",
			SchemaHash:  "jkl012",
			Name:        "Bulk Update Records",
			Description: "Updates multiple records in bulk",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"table": {"type": "string"},
					"filter": {"type": "object"},
					"updates": {"type": "object"},
					"limit": {"type": "integer", "minimum": 1, "maximum": 10000}
				},
				"required": ["table", "filter", "updates"],
				"additionalProperties": false
			}`),
			RiskProfile: types.RiskProfile{
				MoneyMovement:   false,
				PrivilegeChange: false,
				Irreversible:    false,
				BulkOperation:   true,
				ResourceScope:   "tenant",
				DataClass:       "internal",
			},
		},
		{
			ActionID:    "read_file",
			Version:     "1.0.0",
			SchemaHash:  "mno345",
			Name:        "Read File",
			Description: "Reads contents of a file",
			Schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {"type": "string"},
					"encoding": {"type": "string", "default": "utf-8"}
				},
				"required": ["path"],
				"additionalProperties": false
			}`),
			RiskProfile: types.RiskProfile{
				MoneyMovement:   false,
				PrivilegeChange: false,
				Irreversible:    false,
				BulkOperation:   false,
				ResourceScope:   "single",
				DataClass:       "public",
			},
		},
	}

	for _, tool := range sampleTools {
		_ = store.PutTool(context.Background(), tool)
	}

	return store
}

func (s *InMemoryStore) key(actionID, version string) string {
	return fmt.Sprintf("%s:%s", actionID, version)
}

func (s *InMemoryStore) GetTool(ctx context.Context, actionID, version string) (*types.ToolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// If no version specified, find latest
	if version == "" {
		var latest *types.ToolRegistryEntry
		for _, tool := range s.tools {
			if tool.ActionID == actionID {
				if latest == nil || tool.Version > latest.Version {
					latest = tool
				}
			}
		}
		if latest == nil {
			return nil, ErrToolNotFound
		}
		return latest, nil
	}

	tool, ok := s.tools[s.key(actionID, version)]
	if !ok {
		return nil, ErrToolNotFound
	}
	return tool, nil
}

func (s *InMemoryStore) GetToolBySchemaHash(ctx context.Context, actionID, schemaHash string) (*types.ToolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, tool := range s.tools {
		if tool.ActionID == actionID && tool.SchemaHash == schemaHash {
			return tool, nil
		}
	}
	return nil, ErrToolNotFound
}

func (s *InMemoryStore) ListTools(ctx context.Context) ([]*types.ToolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]*types.ToolRegistryEntry, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}
	return tools, nil
}

func (s *InMemoryStore) PutTool(ctx context.Context, entry *types.ToolRegistryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tools[s.key(entry.ActionID, entry.Version)] = entry
	return nil
}

func (s *InMemoryStore) DeleteTool(ctx context.Context, actionID, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tools, s.key(actionID, version))
	return nil
}

// S3Store is a stub for S3-backed registry storage.
// TODO: Implement full S3 support.
type S3Store struct {
	bucket string
	region string
	// TODO: Add S3 client
}

// NewS3Store creates a new S3-backed registry store.
func NewS3Store(bucket, region string) *S3Store {
	return &S3Store{
		bucket: bucket,
		region: region,
	}
}

func (s *S3Store) GetTool(ctx context.Context, actionID, version string) (*types.ToolRegistryEntry, error) {
	// TODO: Implement S3 fetch
	// Key format: tools/{actionID}/{version}/manifest.json
	return nil, fmt.Errorf("S3Store.GetTool not implemented")
}

func (s *S3Store) GetToolBySchemaHash(ctx context.Context, actionID, schemaHash string) (*types.ToolRegistryEntry, error) {
	// TODO: Implement S3 fetch with schema hash lookup
	return nil, fmt.Errorf("S3Store.GetToolBySchemaHash not implemented")
}

func (s *S3Store) ListTools(ctx context.Context) ([]*types.ToolRegistryEntry, error) {
	// TODO: Implement S3 list
	return nil, fmt.Errorf("S3Store.ListTools not implemented")
}

func (s *S3Store) PutTool(ctx context.Context, entry *types.ToolRegistryEntry) error {
	// TODO: Implement S3 put
	return fmt.Errorf("S3Store.PutTool not implemented")
}

func (s *S3Store) DeleteTool(ctx context.Context, actionID, version string) error {
	// TODO: Implement S3 delete
	return fmt.Errorf("S3Store.DeleteTool not implemented")
}
