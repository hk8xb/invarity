// Package policy provides policy storage and evaluation.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"invarity/internal/types"
)

// ErrPolicyNotFound is returned when a policy bundle is not found.
var ErrPolicyNotFound = fmt.Errorf("policy bundle not found")

// Store defines the interface for policy bundle storage.
type Store interface {
	// GetCompiledPolicy retrieves the compiled policy bundle for an organization.
	GetCompiledPolicy(ctx context.Context, orgID string) (*types.PolicyBundle, error)

	// PutCompiledPolicy stores a compiled policy bundle.
	PutCompiledPolicy(ctx context.Context, bundle *types.PolicyBundle) error
}

// CachedStore wraps a Store with TTL-based caching.
type CachedStore struct {
	store Store
	cache map[string]*cacheEntry
	mu    sync.RWMutex
	ttl   time.Duration
}

type cacheEntry struct {
	bundle    *types.PolicyBundle
	expiresAt time.Time
}

// NewCachedStore creates a new cached policy store.
func NewCachedStore(store Store, ttl time.Duration) *CachedStore {
	return &CachedStore{
		store: store,
		cache: make(map[string]*cacheEntry),
		ttl:   ttl,
	}
}

func (s *CachedStore) GetCompiledPolicy(ctx context.Context, orgID string) (*types.PolicyBundle, error) {
	s.mu.RLock()
	entry, ok := s.cache[orgID]
	s.mu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		return entry.bundle, nil
	}

	bundle, err := s.store.GetCompiledPolicy(ctx, orgID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[orgID] = &cacheEntry{
		bundle:    bundle,
		expiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()

	return bundle, nil
}

func (s *CachedStore) PutCompiledPolicy(ctx context.Context, bundle *types.PolicyBundle) error {
	if err := s.store.PutCompiledPolicy(ctx, bundle); err != nil {
		return err
	}

	s.mu.Lock()
	s.cache[bundle.OrgID] = &cacheEntry{
		bundle:    bundle,
		expiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()

	return nil
}

// Invalidate removes a policy from the cache.
func (s *CachedStore) Invalidate(orgID string) {
	s.mu.Lock()
	delete(s.cache, orgID)
	s.mu.Unlock()
}

// InMemoryStore is an in-memory implementation of Store.
type InMemoryStore struct {
	mu       sync.RWMutex
	policies map[string]*types.PolicyBundle
}

// NewInMemoryStore creates a new in-memory policy store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		policies: make(map[string]*types.PolicyBundle),
	}
}

// NewInMemoryStoreWithDefaults creates a store with sample policies.
func NewInMemoryStoreWithDefaults() *InMemoryStore {
	store := NewInMemoryStore()

	// Add sample policy bundles
	defaultBundle := &types.PolicyBundle{
		OrgID:   "default",
		Version: "1.0.0",
		Rules: []types.PolicyRule{
			{
				ID:          "deny-production-delete",
				Name:        "Deny Delete in Production",
				Description: "Deny all delete operations in production without approval",
				Priority:    100,
				Conditions: json.RawMessage(`{
					"all": [
						{"env": "production"},
						{"action_prefix": "delete"},
						{"not": {"has_approval": true}}
					]
				}`),
				Effect: "deny",
			},
			{
				ID:          "require-approval-high-risk",
				Name:        "Require Approval for High Risk",
				Description: "Escalate high-risk operations for human approval",
				Priority:    90,
				Conditions: json.RawMessage(`{
					"any": [
						{"risk_level": "HIGH"},
						{"risk_level": "CRITICAL"}
					]
				}`),
				Effect: "escalate",
			},
			{
				ID:          "allow-read-operations",
				Name:        "Allow Read Operations",
				Description: "Allow all read-only operations",
				Priority:    50,
				Conditions: json.RawMessage(`{
					"action_prefix": "read"
				}`),
				Effect: "allow",
			},
			{
				ID:          "money-movement-verification",
				Name:        "Money Movement Verification",
				Description: "Require facts for money movement over threshold",
				Priority:    85,
				Conditions: json.RawMessage(`{
					"all": [
						{"money_movement": true},
						{"amount_gt": 10000}
					]
				}`),
				Effect:       "escalate",
				RequiresFct: []string{"verified_recipient", "compliance_check"},
			},
		},
		ClauseIndex: []string{
			"deny-production-delete",
			"require-approval-high-risk",
			"allow-read-operations",
			"money-movement-verification",
		},
		CompiledAt: time.Now(),
	}

	_ = store.PutCompiledPolicy(context.Background(), defaultBundle)

	return store
}

func (s *InMemoryStore) GetCompiledPolicy(ctx context.Context, orgID string) (*types.PolicyBundle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bundle, ok := s.policies[orgID]
	if !ok {
		// Fall back to default policy
		bundle, ok = s.policies["default"]
		if !ok {
			return nil, ErrPolicyNotFound
		}
	}
	return bundle, nil
}

func (s *InMemoryStore) PutCompiledPolicy(ctx context.Context, bundle *types.PolicyBundle) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.policies[bundle.OrgID] = bundle
	return nil
}

// S3Store is a stub for S3-backed policy storage.
type S3Store struct {
	bucket string
	region string
}

// NewS3Store creates a new S3-backed policy store.
func NewS3Store(bucket, region string) *S3Store {
	return &S3Store{
		bucket: bucket,
		region: region,
	}
}

func (s *S3Store) GetCompiledPolicy(ctx context.Context, orgID string) (*types.PolicyBundle, error) {
	// TODO: Implement S3 fetch
	// Key format: policies/{orgID}/compiled.json
	return nil, fmt.Errorf("S3Store.GetCompiledPolicy not implemented")
}

func (s *S3Store) PutCompiledPolicy(ctx context.Context, bundle *types.PolicyBundle) error {
	// TODO: Implement S3 put
	return fmt.Errorf("S3Store.PutCompiledPolicy not implemented")
}
