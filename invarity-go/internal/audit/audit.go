// Package audit provides audit record storage.
package audit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"invarity/internal/types"
)

// Store defines the interface for audit record storage.
type Store interface {
	// Write stores an audit record and returns the audit ID.
	Write(ctx context.Context, record *types.AuditRecord) (string, error)

	// Get retrieves an audit record by ID.
	Get(ctx context.Context, auditID string) (*types.AuditRecord, error)

	// List retrieves audit records with optional filters.
	List(ctx context.Context, filter *ListFilter) ([]*types.AuditRecord, error)
}

// ListFilter contains optional filters for listing audit records.
type ListFilter struct {
	OrgID     string
	ActorID   string
	ActionID  string
	Decision  types.Decision
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
}

// InMemoryStore is an in-memory implementation of Store.
type InMemoryStore struct {
	mu      sync.RWMutex
	records map[string]*types.AuditRecord
	order   []string // Maintains insertion order
}

// NewInMemoryStore creates a new in-memory audit store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		records: make(map[string]*types.AuditRecord),
		order:   make([]string, 0),
	}
}

func (s *InMemoryStore) Write(ctx context.Context, record *types.AuditRecord) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.AuditID == "" {
		record.AuditID = uuid.New().String()
	}

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	s.records[record.AuditID] = record
	s.order = append(s.order, record.AuditID)

	return record.AuditID, nil
}

func (s *InMemoryStore) Get(ctx context.Context, auditID string) (*types.AuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.records[auditID]
	if !ok {
		return nil, fmt.Errorf("audit record not found: %s", auditID)
	}

	return record, nil
}

func (s *InMemoryStore) List(ctx context.Context, filter *ListFilter) ([]*types.AuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.AuditRecord, 0)

	// Iterate in reverse order (newest first)
	for i := len(s.order) - 1; i >= 0; i-- {
		record := s.records[s.order[i]]

		if filter != nil {
			if filter.OrgID != "" && record.OrgID != filter.OrgID {
				continue
			}
			if filter.ActorID != "" && record.Actor.ID != filter.ActorID {
				continue
			}
			if filter.ActionID != "" && record.ToolCall.ActionID != filter.ActionID {
				continue
			}
			if filter.Decision != "" && record.Decision != filter.Decision {
				continue
			}
			if !filter.StartTime.IsZero() && record.CreatedAt.Before(filter.StartTime) {
				continue
			}
			if !filter.EndTime.IsZero() && record.CreatedAt.After(filter.EndTime) {
				continue
			}
		}

		result = append(result, record)

		if filter != nil && filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}

	return result, nil
}

// S3Store is a stub for S3-backed audit storage.
type S3Store struct {
	bucket string
	region string
	prefix string
}

// NewS3Store creates a new S3-backed audit store.
func NewS3Store(bucket, region, prefix string) *S3Store {
	if prefix == "" {
		prefix = "audit"
	}
	return &S3Store{
		bucket: bucket,
		region: region,
		prefix: prefix,
	}
}

func (s *S3Store) Write(ctx context.Context, record *types.AuditRecord) (string, error) {
	// TODO: Implement S3 write
	// Key format: {prefix}/{org_id}/{year}/{month}/{day}/{audit_id}.json
	// Consider using a buffer/batch writer for high throughput

	if record.AuditID == "" {
		record.AuditID = uuid.New().String()
	}

	return record.AuditID, fmt.Errorf("S3Store.Write not implemented")
}

func (s *S3Store) Get(ctx context.Context, auditID string) (*types.AuditRecord, error) {
	// TODO: Implement S3 read
	// This requires knowing the org_id and date to construct the key
	// Consider maintaining an index (DynamoDB) for lookups by audit_id
	return nil, fmt.Errorf("S3Store.Get not implemented")
}

func (s *S3Store) List(ctx context.Context, filter *ListFilter) ([]*types.AuditRecord, error) {
	// TODO: Implement S3 list with prefix scanning
	// This is expensive for S3; consider using an index for queries
	return nil, fmt.Errorf("S3Store.List not implemented")
}

// Writer wraps a Store and provides convenience methods.
type Writer struct {
	store Store
}

// NewWriter creates a new audit writer.
func NewWriter(store Store) *Writer {
	return &Writer{store: store}
}

// WriteFromResponse creates and stores an audit record from a firewall response.
func (w *Writer) WriteFromResponse(
	ctx context.Context,
	req *types.ToolCallRequest,
	resp *types.FirewallDecisionResponse,
	pipelineStep string,
) (string, error) {
	record := &types.AuditRecord{
		RequestID:    resp.RequestID,
		OrgID:        req.OrgID,
		Actor:        req.Actor,
		Environment:  req.Environment,
		ToolCall:     req.ToolCall,
		UserIntent:   req.UserIntent,
		Decision:     resp.Decision,
		RiskTier:     resp.RiskTier,
		Reasons:      resp.Reasons,
		Constraints:  resp.Constraints,
		Alignment:    resp.Alignment,
		Threat:       resp.Threat,
		Timing:       resp.Timing,
		PipelineStep: pipelineStep,
	}

	return w.store.Write(ctx, record)
}
