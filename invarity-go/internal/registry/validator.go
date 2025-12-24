// Package registry provides tool registry interfaces and implementations.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"invarity/internal/types"
)

// ValidationError represents a schema validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "; ")
}

// SchemaValidator validates tool call arguments against JSON schemas.
type SchemaValidator struct {
	compiler *jsonschema.Compiler
	cache    map[string]*jsonschema.Schema
}

// NewSchemaValidator creates a new schema validator.
func NewSchemaValidator() *SchemaValidator {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	return &SchemaValidator{
		compiler: compiler,
		cache:    make(map[string]*jsonschema.Schema),
	}
}

// ValidateArgs validates tool call arguments against the tool's schema.
func (v *SchemaValidator) ValidateArgs(ctx context.Context, tool *types.ToolRegistryEntry, args json.RawMessage) error {
	// Get or compile schema
	schema, err := v.getSchema(tool)
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	// Parse args
	var argsData any
	if err := json.Unmarshal(args, &argsData); err != nil {
		return ValidationErrors{{Field: "args", Message: "invalid JSON"}}
	}

	// Validate
	if err := schema.Validate(argsData); err != nil {
		return v.convertValidationError(err)
	}

	return nil
}

func (v *SchemaValidator) getSchema(tool *types.ToolRegistryEntry) (*jsonschema.Schema, error) {
	cacheKey := fmt.Sprintf("%s:%s", tool.ActionID, tool.SchemaHash)

	if schema, ok := v.cache[cacheKey]; ok {
		return schema, nil
	}

	// Parse the schema
	schemaURL := fmt.Sprintf("mem://%s/%s/schema.json", tool.ActionID, tool.Version)

	if err := v.compiler.AddResource(schemaURL, strings.NewReader(string(tool.Schema))); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	schema, err := v.compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	v.cache[cacheKey] = schema
	return schema, nil
}

func (v *SchemaValidator) convertValidationError(err error) error {
	validationErr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return ValidationErrors{{Field: "unknown", Message: err.Error()}}
	}

	var errors ValidationErrors
	v.collectErrors(validationErr, &errors)

	if len(errors) == 0 {
		errors = append(errors, ValidationError{
			Field:   "unknown",
			Message: err.Error(),
		})
	}

	return errors
}

func (v *SchemaValidator) collectErrors(err *jsonschema.ValidationError, errors *ValidationErrors) {
	if err.Message != "" {
		*errors = append(*errors, ValidationError{
			Field:   err.InstanceLocation,
			Message: err.Message,
		})
	}

	for _, cause := range err.Causes {
		v.collectErrors(cause, errors)
	}
}

// ValidateToolExists checks if a tool exists and matches the requested version/schema.
func ValidateToolExists(ctx context.Context, store Store, toolCall types.ToolCall) (*types.ToolRegistryEntry, error) {
	var tool *types.ToolRegistryEntry
	var err error

	// Prefer schema hash lookup if provided
	if toolCall.SchemaHash != "" {
		tool, err = store.GetToolBySchemaHash(ctx, toolCall.ActionID, toolCall.SchemaHash)
		if err == nil {
			return tool, nil
		}
		if err != ErrToolNotFound {
			return nil, fmt.Errorf("registry lookup failed: %w", err)
		}
		// If schema hash not found, it's a mismatch
		return nil, ErrVersionMismatch
	}

	// Fall back to version lookup
	tool, err = store.GetTool(ctx, toolCall.ActionID, toolCall.Version)
	if err != nil {
		if err == ErrToolNotFound {
			return nil, err
		}
		return nil, fmt.Errorf("registry lookup failed: %w", err)
	}

	// If version was specified, verify it matches
	if toolCall.Version != "" && tool.Version != toolCall.Version {
		return nil, ErrVersionMismatch
	}

	return tool, nil
}
