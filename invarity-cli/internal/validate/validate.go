// Package validate provides JSON Schema validation for Invarity tool and toolset manifests.
package validate

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

//go:embed schemas/*.json
var embeddedSchemas embed.FS

// Validator provides JSON Schema validation for tool manifests.
type Validator struct {
	schema *jsonschema.Schema
}

// ToolsetValidator provides JSON Schema validation for toolset manifests.
type ToolsetValidator struct {
	schema *jsonschema.Schema
}

// ValidationError represents a validation error with details.
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return e.Message
}

// ValidationResult contains the result of a validation.
type ValidationResult struct {
	Valid  bool               `json:"valid"`
	Errors []*ValidationError `json:"errors,omitempty"`
}

// NewValidator creates a new Validator with the embedded tool schema.
func NewValidator() (*Validator, error) {
	// Read embedded schema
	schemaData, err := embeddedSchemas.ReadFile("schemas/invarity.tool.schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded schema: %w", err)
	}

	// Compile schema
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	if err := compiler.AddResource("invarity.tool.schema.json", strings.NewReader(string(schemaData))); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	schema, err := compiler.Compile("invarity.tool.schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	return &Validator{schema: schema}, nil
}

// NewToolsetValidator creates a new ToolsetValidator with the embedded toolset schema.
func NewToolsetValidator() (*ToolsetValidator, error) {
	schemaData, err := embeddedSchemas.ReadFile("schemas/invarity.toolset.schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded toolset schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	if err := compiler.AddResource("invarity.toolset.schema.json", strings.NewReader(string(schemaData))); err != nil {
		return nil, fmt.Errorf("failed to add toolset schema resource: %w", err)
	}

	schema, err := compiler.Compile("invarity.toolset.schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile toolset schema: %w", err)
	}

	return &ToolsetValidator{schema: schema}, nil
}

// ValidateFile validates a tool manifest file (YAML or JSON).
func (v *Validator) ValidateFile(filePath string) (*ValidationResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".yaml", ".yml":
		return v.ValidateYAML(data)
	case ".json":
		return v.ValidateJSON(data)
	default:
		return nil, fmt.Errorf("unsupported file extension: %s (use .yaml, .yml, or .json)", ext)
	}
}

// ValidateYAML validates YAML data against the tool schema.
func (v *Validator) ValidateYAML(data []byte) (*ValidationResult, error) {
	var obj interface{}
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return &ValidationResult{
			Valid: false,
			Errors: []*ValidationError{
				{Message: fmt.Sprintf("invalid YAML: %v", err)},
			},
		}, nil
	}

	jsonCompatible := convertYAMLToJSON(obj)
	return v.validate(jsonCompatible)
}

// ValidateJSON validates JSON data against the tool schema.
func (v *Validator) ValidateJSON(data []byte) (*ValidationResult, error) {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return &ValidationResult{
			Valid: false,
			Errors: []*ValidationError{
				{Message: fmt.Sprintf("invalid JSON: %v", err)},
			},
		}, nil
	}

	return v.validate(obj)
}

// validate performs the actual schema validation.
func (v *Validator) validate(obj interface{}) (*ValidationResult, error) {
	err := v.schema.Validate(obj)
	if err == nil {
		return &ValidationResult{Valid: true}, nil
	}

	var validationErrors []*ValidationError
	if validationErr, ok := err.(*jsonschema.ValidationError); ok {
		validationErrors = extractErrors(validationErr)
	} else {
		validationErrors = []*ValidationError{
			{Message: err.Error()},
		}
	}

	return &ValidationResult{
		Valid:  false,
		Errors: validationErrors,
	}, nil
}

// ValidateFile validates a toolset manifest file (YAML or JSON).
func (v *ToolsetValidator) ValidateFile(filePath string) (*ValidationResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".yaml", ".yml":
		return v.ValidateYAML(data)
	case ".json":
		return v.ValidateJSON(data)
	default:
		return nil, fmt.Errorf("unsupported file extension: %s (use .yaml, .yml, or .json)", ext)
	}
}

// ValidateYAML validates YAML data against the toolset schema.
func (v *ToolsetValidator) ValidateYAML(data []byte) (*ValidationResult, error) {
	var obj interface{}
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return &ValidationResult{
			Valid: false,
			Errors: []*ValidationError{
				{Message: fmt.Sprintf("invalid YAML: %v", err)},
			},
		}, nil
	}

	jsonCompatible := convertYAMLToJSON(obj)
	return v.validate(jsonCompatible)
}

// ValidateJSON validates JSON data against the toolset schema.
func (v *ToolsetValidator) ValidateJSON(data []byte) (*ValidationResult, error) {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return &ValidationResult{
			Valid: false,
			Errors: []*ValidationError{
				{Message: fmt.Sprintf("invalid JSON: %v", err)},
			},
		}, nil
	}

	return v.validate(obj)
}

// validate performs the actual schema validation.
func (v *ToolsetValidator) validate(obj interface{}) (*ValidationResult, error) {
	err := v.schema.Validate(obj)
	if err == nil {
		return &ValidationResult{Valid: true}, nil
	}

	var validationErrors []*ValidationError
	if validationErr, ok := err.(*jsonschema.ValidationError); ok {
		validationErrors = extractErrors(validationErr)
	} else {
		validationErrors = []*ValidationError{
			{Message: err.Error()},
		}
	}

	return &ValidationResult{
		Valid:  false,
		Errors: validationErrors,
	}, nil
}

// extractErrors recursively extracts validation errors from the schema validation result.
func extractErrors(err *jsonschema.ValidationError) []*ValidationError {
	var errors []*ValidationError

	if len(err.Causes) == 0 {
		errors = append(errors, &ValidationError{
			Path:    err.InstanceLocation,
			Message: err.Message,
		})
	}

	for _, cause := range err.Causes {
		errors = append(errors, extractErrors(cause)...)
	}

	return errors
}

// convertYAMLToJSON converts YAML-parsed data to JSON-compatible structures.
func convertYAMLToJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[k] = convertYAMLToJSON(v)
		}
		return result
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[fmt.Sprintf("%v", k)] = convertYAMLToJSON(v)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = convertYAMLToJSON(v)
		}
		return result
	default:
		return v
	}
}

// ParseToolFile parses a tool manifest file and returns it as a map.
func ParseToolFile(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	var obj map[string]interface{}

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &obj); err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		converted := convertYAMLToJSON(obj)
		obj = converted.(map[string]interface{})
	case ".json":
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	return obj, nil
}

// ParseToolsetFile parses a toolset manifest file and returns it as a map.
func ParseToolsetFile(filePath string) (map[string]interface{}, error) {
	return ParseToolFile(filePath) // Same parsing logic
}

// ParseRequestFile parses a request JSON file.
func ParseRequestFile(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	return obj, nil
}

// ParsePolicyFile parses a policy YAML file.
func ParsePolicyFile(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	var obj map[string]interface{}

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &obj); err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		converted := convertYAMLToJSON(obj)
		obj = converted.(map[string]interface{})
	case ".json":
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported file extension: %s (use .yaml or .json)", ext)
	}

	return obj, nil
}

// ToolRef represents a tool reference with id and version.
type ToolRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// ParsedTool represents a parsed tool with its invarity metadata.
type ParsedTool struct {
	FilePath string
	Name     string
	ID       string
	Version  string
	Raw      map[string]interface{}
}

// ParseToolWithMetadata parses a tool file and extracts invarity metadata.
func ParseToolWithMetadata(filePath string) (*ParsedTool, error) {
	data, err := ParseToolFile(filePath)
	if err != nil {
		return nil, err
	}

	tool := &ParsedTool{
		FilePath: filePath,
		Raw:      data,
	}

	if name, ok := data["name"].(string); ok {
		tool.Name = name
	}

	if invarity, ok := data["invarity"].(map[string]interface{}); ok {
		if id, ok := invarity["id"].(string); ok {
			tool.ID = id
		}
		if version, ok := invarity["version"].(string); ok {
			tool.Version = version
		}
	}

	return tool, nil
}

// FindToolFiles finds all tool files (*.json, *.yaml, *.yml) in a directory recursively.
func FindToolFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".json" || ext == ".yaml" || ext == ".yml" {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return files, nil
}

// Toolset represents a parsed toolset.
type Toolset struct {
	ToolsetID   string            `json:"toolset_id"`
	Description string            `json:"description,omitempty"`
	Envs        []string          `json:"envs,omitempty"`
	Status      string            `json:"status,omitempty"`
	Tools       []ToolRef         `json:"tools"`
	Policy      *ToolsetPolicy    `json:"policy,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Raw         map[string]interface{}
}

// ToolsetPolicy represents the policy reference in a toolset.
type ToolsetPolicy struct {
	BundleID string `json:"bundle_id,omitempty"`
	Version  string `json:"version,omitempty"`
}

// ParseToolsetWithMetadata parses a toolset file and extracts metadata.
func ParseToolsetWithMetadata(filePath string) (*Toolset, error) {
	data, err := ParseToolsetFile(filePath)
	if err != nil {
		return nil, err
	}

	toolset := &Toolset{Raw: data}

	if id, ok := data["toolset_id"].(string); ok {
		toolset.ToolsetID = id
	}
	if desc, ok := data["description"].(string); ok {
		toolset.Description = desc
	}
	if status, ok := data["status"].(string); ok {
		toolset.Status = status
	}

	if envs, ok := data["envs"].([]interface{}); ok {
		for _, e := range envs {
			if s, ok := e.(string); ok {
				toolset.Envs = append(toolset.Envs, s)
			}
		}
	}

	if tools, ok := data["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tm, ok := t.(map[string]interface{}); ok {
				ref := ToolRef{}
				if id, ok := tm["id"].(string); ok {
					ref.ID = id
				}
				if ver, ok := tm["version"].(string); ok {
					ref.Version = ver
				}
				toolset.Tools = append(toolset.Tools, ref)
			}
		}
	}

	if policy, ok := data["policy"].(map[string]interface{}); ok {
		toolset.Policy = &ToolsetPolicy{}
		if bid, ok := policy["bundle_id"].(string); ok {
			toolset.Policy.BundleID = bid
		}
		if ver, ok := policy["version"].(string); ok {
			toolset.Policy.Version = ver
		}
	}

	if labels, ok := data["labels"].(map[string]interface{}); ok {
		toolset.Labels = make(map[string]string)
		for k, v := range labels {
			if s, ok := v.(string); ok {
				toolset.Labels[k] = s
			}
		}
	}

	return toolset, nil
}

// LintResult contains the result of a toolset lint operation.
type LintResult struct {
	Valid           bool        `json:"valid"`
	MissingTools    []ToolRef   `json:"missing_tools,omitempty"`
	ExtraTools      []ToolRef   `json:"extra_tools,omitempty"`
	InvalidTools    []string    `json:"invalid_tools,omitempty"`
	ToolsWithoutID  []string    `json:"tools_without_id,omitempty"`
	Errors          []string    `json:"errors,omitempty"`
}

// LintToolset checks if all tool references in a toolset exist in the tools directory.
func LintToolset(toolset *Toolset, toolsDir string) (*LintResult, error) {
	result := &LintResult{Valid: true}

	// Find all tool files
	toolFiles, err := FindToolFiles(toolsDir)
	if err != nil {
		return nil, err
	}

	// Build map of available tools
	availableTools := make(map[string]*ParsedTool) // key: "id@version"
	for _, file := range toolFiles {
		tool, err := ParseToolWithMetadata(file)
		if err != nil {
			result.InvalidTools = append(result.InvalidTools, file)
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", file, err))
			continue
		}

		if tool.ID == "" || tool.Version == "" {
			result.ToolsWithoutID = append(result.ToolsWithoutID, file)
			continue
		}

		key := fmt.Sprintf("%s@%s", tool.ID, tool.Version)
		availableTools[key] = tool
	}

	// Check for missing tools
	referencedKeys := make(map[string]bool)
	for _, ref := range toolset.Tools {
		key := fmt.Sprintf("%s@%s", ref.ID, ref.Version)
		referencedKeys[key] = true

		if _, exists := availableTools[key]; !exists {
			result.MissingTools = append(result.MissingTools, ref)
			result.Valid = false
		}
	}

	// Check for extra tools (in directory but not referenced)
	for key, tool := range availableTools {
		if !referencedKeys[key] {
			result.ExtraTools = append(result.ExtraTools, ToolRef{
				ID:      tool.ID,
				Version: tool.Version,
			})
		}
	}

	// Mark invalid if there are tools without IDs
	if len(result.ToolsWithoutID) > 0 {
		result.Valid = false
	}
	if len(result.InvalidTools) > 0 {
		result.Valid = false
	}

	return result, nil
}
