// Package policy provides policy validation and canonical rendering.
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidationReport contains the result of policy validation.
type ValidationReport struct {
	Valid    bool               `json:"valid"`
	Errors   []ValidationError  `json:"errors,omitempty"`
	Warnings []ValidationError  `json:"warnings,omitempty"`
	Summary  *ValidationSummary `json:"summary,omitempty"`
}

// ValidationError represents a single validation issue.
type ValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

// ValidationSummary provides a summary of the validated policy.
type ValidationSummary struct {
	APIVersion string `json:"api_version,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Version    string `json:"version,omitempty"`
	RuleCount  int    `json:"rule_count"`
}

// Policy represents a parsed policy document.
type Policy struct {
	APIVersion string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                 `yaml:"kind" json:"kind"`
	Metadata   PolicyMetadata         `yaml:"metadata" json:"metadata"`
	Spec       map[string]interface{} `yaml:"spec" json:"spec"`
	Raw        map[string]interface{} `yaml:"-" json:"-"`
}

// PolicyMetadata contains policy metadata.
type PolicyMetadata struct {
	Name        string            `yaml:"name" json:"name"`
	Version     string            `yaml:"version" json:"version,omitempty"`
	Description string            `yaml:"description" json:"description,omitempty"`
	Labels      map[string]string `yaml:"labels" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations" json:"annotations,omitempty"`
}

// ValidateFile validates a policy file and returns a validation report.
func ValidateFile(filePath string) (*ValidationReport, *Policy, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".yaml", ".yml":
		return ValidateYAML(data)
	case ".json":
		return ValidateJSON(data)
	default:
		return nil, nil, fmt.Errorf("unsupported file extension: %s (use .yaml, .yml, or .json)", ext)
	}
}

// ValidateYAML validates YAML policy data.
func ValidateYAML(data []byte) (*ValidationReport, *Policy, error) {
	report := &ValidationReport{Valid: true}

	// Parse YAML
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Message: fmt.Sprintf("invalid YAML syntax: %v", err),
		})
		return report, nil, nil
	}

	// Parse into structured policy
	var policy Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Message: fmt.Sprintf("failed to parse policy structure: %v", err),
		})
		return report, nil, nil
	}
	policy.Raw = raw

	// Validate required fields
	validateRequiredFields(raw, report, &policy)

	return report, &policy, nil
}

// ValidateJSON validates JSON policy data.
func ValidateJSON(data []byte) (*ValidationReport, *Policy, error) {
	// Convert JSON to YAML for unified processing
	// For now, just re-encode as YAML
	return ValidateYAML(data)
}

func validateRequiredFields(raw map[string]interface{}, report *ValidationReport, policy *Policy) {
	// Check apiVersion
	if _, ok := raw["apiVersion"]; !ok {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "apiVersion",
			Message: "required field 'apiVersion' is missing",
		})
	} else if policy.APIVersion == "" {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "apiVersion",
			Message: "field 'apiVersion' cannot be empty",
		})
	} else if !strings.HasPrefix(policy.APIVersion, "invarity.dev/") {
		report.Warnings = append(report.Warnings, ValidationError{
			Field:   "apiVersion",
			Message: fmt.Sprintf("apiVersion '%s' may not be recognized; expected 'invarity.dev/v1'", policy.APIVersion),
		})
	}

	// Check kind
	if _, ok := raw["kind"]; !ok {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "kind",
			Message: "required field 'kind' is missing",
		})
	} else if policy.Kind == "" {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "kind",
			Message: "field 'kind' cannot be empty",
		})
	} else if policy.Kind != "Policy" {
		report.Warnings = append(report.Warnings, ValidationError{
			Field:   "kind",
			Message: fmt.Sprintf("kind '%s' may not be recognized; expected 'Policy'", policy.Kind),
		})
	}

	// Check metadata
	if _, ok := raw["metadata"]; !ok {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "metadata",
			Message: "required field 'metadata' is missing",
		})
	} else {
		if policy.Metadata.Name == "" {
			report.Valid = false
			report.Errors = append(report.Errors, ValidationError{
				Field:   "metadata.name",
				Message: "required field 'metadata.name' is missing or empty",
			})
		}
	}

	// Check spec
	if _, ok := raw["spec"]; !ok {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "spec",
			Message: "required field 'spec' is missing",
		})
	} else if policy.Spec != nil {
		// Validate spec contents
		validateSpec(policy.Spec, report)
	}

	// Build summary
	ruleCount := 0
	if policy.Spec != nil {
		if rules, ok := policy.Spec["rules"].([]interface{}); ok {
			ruleCount = len(rules)
		}
	}

	report.Summary = &ValidationSummary{
		APIVersion: policy.APIVersion,
		Kind:       policy.Kind,
		Name:       policy.Metadata.Name,
		Version:    policy.Metadata.Version,
		RuleCount:  ruleCount,
	}
}

func validateSpec(spec map[string]interface{}, report *ValidationReport) {
	// Check for rules (optional but recommended)
	if _, ok := spec["rules"]; !ok {
		report.Warnings = append(report.Warnings, ValidationError{
			Field:   "spec.rules",
			Message: "no rules defined in policy; policy may have no effect",
		})
	} else {
		if rules, ok := spec["rules"].([]interface{}); ok {
			for i, rule := range rules {
				validateRule(i, rule, report)
			}
		}
	}

	// Check defaultAction
	if defaultAction, ok := spec["defaultAction"]; ok {
		action := fmt.Sprintf("%v", defaultAction)
		validActions := []string{"allow", "deny", "escalate", "review"}
		isValid := false
		for _, va := range validActions {
			if strings.EqualFold(action, va) {
				isValid = true
				break
			}
		}
		if !isValid {
			report.Warnings = append(report.Warnings, ValidationError{
				Field:   "spec.defaultAction",
				Message: fmt.Sprintf("defaultAction '%s' may not be recognized; expected one of: allow, deny, escalate, review", action),
			})
		}
	}
}

func validateRule(index int, rule interface{}, report *ValidationReport) {
	ruleMap, ok := rule.(map[string]interface{})
	if !ok {
		report.Errors = append(report.Errors, ValidationError{
			Field:   fmt.Sprintf("spec.rules[%d]", index),
			Message: "rule must be an object",
		})
		report.Valid = false
		return
	}

	// Check required rule fields
	if _, ok := ruleMap["name"]; !ok {
		report.Warnings = append(report.Warnings, ValidationError{
			Field:   fmt.Sprintf("spec.rules[%d].name", index),
			Message: "rule is missing 'name' field; recommended for debugging",
		})
	}

	if _, ok := ruleMap["condition"]; !ok {
		report.Warnings = append(report.Warnings, ValidationError{
			Field:   fmt.Sprintf("spec.rules[%d].condition", index),
			Message: "rule is missing 'condition' field; rule will always match",
		})
	}

	if _, ok := ruleMap["action"]; !ok {
		report.Errors = append(report.Errors, ValidationError{
			Field:   fmt.Sprintf("spec.rules[%d].action", index),
			Message: "rule is missing required 'action' field",
		})
		report.Valid = false
	}
}

// ParseFile parses a policy file without validation.
func ParseFile(filePath string) (*Policy, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var policy Policy
	var raw map[string]interface{}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &policy); err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		yaml.Unmarshal(data, &raw)
	case ".json":
		if err := yaml.Unmarshal(data, &policy); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		yaml.Unmarshal(data, &raw)
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	policy.Raw = raw
	return &policy, nil
}
