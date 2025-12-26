// Package firewall implements the Invarity Firewall decision pipeline.
package firewall

import (
	"context"
	"fmt"

	"invarity/internal/store"
	"invarity/internal/types"
)

// ToolResolver resolves tools through the principal -> toolset -> tool chain.
type ToolResolver struct {
	ddbStore *store.DynamoDBStore
	s3Client *store.S3Client
}

// NewToolResolver creates a new tool resolver.
func NewToolResolver(ddbStore *store.DynamoDBStore, s3Client *store.S3Client) *ToolResolver {
	return &ToolResolver{
		ddbStore: ddbStore,
		s3Client: s3Client,
	}
}

// ResolveToolResult contains the result of tool resolution.
type ResolveToolResult struct {
	Tool          *types.ToolManifestV3
	ToolsetID     string
	ToolsetRev    string
	ResolvedVia   string // "toolset", "direct", "legacy"
}

// ResolveTool resolves a tool through the principal's active toolset.
// Resolution order:
// 1. If principal_id provided: principal -> active toolset -> tool refs -> tool manifest
// 2. If no principal_id but tenant_id: direct lookup in tenant's tools
// 3. Fallback to legacy registry lookup (for backwards compatibility)
func (r *ToolResolver) ResolveTool(ctx context.Context, req *types.ToolCallRequest) (*ResolveToolResult, error) {
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = req.OrgID // Backwards compatibility
	}

	toolID := req.ToolCall.ActionID
	version := req.ToolCall.Version

	// Case 1: Principal-based resolution (recommended path)
	if req.PrincipalID != "" && tenantID != "" && r.ddbStore != nil {
		result, err := r.resolveViaPrincipal(ctx, tenantID, req.PrincipalID, toolID, version)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
		// Principal doesn't have toolset assigned, fall through to direct lookup
	}

	// Case 2: Direct tenant lookup
	if tenantID != "" && r.ddbStore != nil {
		result, err := r.resolveDirectFromTenant(ctx, tenantID, toolID, version)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
	}

	// Case 3: No resolution possible with new system
	return nil, fmt.Errorf("tool not found: %s (tenant: %s, principal: %s)", toolID, tenantID, req.PrincipalID)
}

// resolveViaPrincipal resolves a tool through the principal's active toolset.
func (r *ToolResolver) resolveViaPrincipal(ctx context.Context, tenantID, principalID, toolID, version string) (*ResolveToolResult, error) {
	// Get principal's active toolset
	toolsetID, toolsetRev, err := r.ddbStore.GetPrincipalActiveToolset(ctx, principalID)
	if err != nil {
		return nil, fmt.Errorf("failed to get principal active toolset: %w", err)
	}
	if toolsetID == "" {
		// No active toolset assigned
		return nil, nil
	}

	// Get toolset manifest from S3
	s3Key := store.ToolsetManifestKey(tenantID, toolsetID, toolsetRev)
	var toolsetManifest types.ToolsetManifest

	if r.s3Client != nil {
		if err := r.s3Client.GetJSON(ctx, s3Key, &toolsetManifest); err != nil {
			return nil, fmt.Errorf("failed to load toolset manifest: %w", err)
		}
	} else {
		// No S3 client, fall back to checking if tool exists in DynamoDB
		// This is a degraded mode without full toolset validation
		return nil, nil
	}

	// Find the tool in the toolset
	var matchedRef *types.ToolRef
	for _, ref := range toolsetManifest.Tools {
		if ref.ToolID == toolID {
			// If version specified, must match
			if version != "" && ref.Version != version {
				continue
			}
			matchedRef = &ref
			break
		}
	}

	if matchedRef == nil {
		return nil, fmt.Errorf("tool %s not found in principal's active toolset %s", toolID, toolsetID)
	}

	// Load the tool manifest
	tool, err := r.loadToolManifest(ctx, tenantID, matchedRef.ToolID, matchedRef.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to load tool manifest: %w", err)
	}

	return &ResolveToolResult{
		Tool:        tool,
		ToolsetID:   toolsetID,
		ToolsetRev:  toolsetRev,
		ResolvedVia: "toolset",
	}, nil
}

// resolveDirectFromTenant resolves a tool directly from the tenant's tool library.
func (r *ToolResolver) resolveDirectFromTenant(ctx context.Context, tenantID, toolID, version string) (*ResolveToolResult, error) {
	// If no version specified, we need to find the latest
	if version == "" {
		// For now, require explicit version in direct lookup
		// TODO: Implement latest version lookup via DynamoDB query
		return nil, fmt.Errorf("version required for direct tool lookup")
	}

	tool, err := r.loadToolManifest(ctx, tenantID, toolID, version)
	if err != nil {
		return nil, err
	}
	if tool == nil {
		return nil, nil
	}

	return &ResolveToolResult{
		Tool:        tool,
		ResolvedVia: "direct",
	}, nil
}

// loadToolManifest loads a tool manifest from S3 or DynamoDB metadata.
func (r *ToolResolver) loadToolManifest(ctx context.Context, tenantID, toolID, version string) (*types.ToolManifestV3, error) {
	// First check if tool exists in DynamoDB
	record, err := r.ddbStore.GetToolRecord(ctx, tenantID, toolID, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool record: %w", err)
	}
	if record == nil {
		return nil, nil
	}

	// Try to load full manifest from S3
	if r.s3Client != nil {
		var manifest types.ToolManifestV3
		if err := r.s3Client.GetJSON(ctx, record.S3Key, &manifest); err != nil {
			// Fall back to constructing from metadata
			return r.constructManifestFromRecord(record), nil
		}
		return &manifest, nil
	}

	// No S3, construct from DynamoDB metadata
	return r.constructManifestFromRecord(record), nil
}

// constructManifestFromRecord creates a minimal manifest from DynamoDB record.
// This is a fallback when S3 is unavailable.
func (r *ToolResolver) constructManifestFromRecord(record *store.ToolRecord) *types.ToolManifestV3 {
	return &types.ToolManifestV3{
		SchemaVersion: "3",
		ToolID:        record.ToolID,
		Version:       record.Version,
		SchemaHash:    record.SchemaHash,
		Name:          record.Name,
		RiskProfile: types.RiskProfileV3{
			BaseRiskLevel: record.RiskLevel,
		},
	}
}

// ResolvedToolToRegistryEntry converts a resolved tool to a ToolRegistryEntry for pipeline compatibility.
func ResolvedToolToRegistryEntry(tool *types.ToolManifestV3) *types.ToolRegistryEntry {
	if tool == nil {
		return nil
	}
	return tool.ToToolRegistryEntry()
}
