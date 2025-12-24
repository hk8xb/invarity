package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CanonicalRender produces a canonical YAML representation of a policy
// with stable key ordering for diffing purposes.
func CanonicalRender(policy *Policy) (string, error) {
	if policy == nil || policy.Raw == nil {
		return "", fmt.Errorf("policy is nil or has no raw data")
	}

	// Create ordered representation
	ordered := canonicalizeMap(policy.Raw)

	// Marshal to YAML
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(ordered); err != nil {
		return "", fmt.Errorf("failed to encode canonical YAML: %w", err)
	}
	encoder.Close()

	return buf.String(), nil
}

// CanonicalJSON produces a canonical JSON representation with sorted keys.
func CanonicalJSON(policy *Policy) (string, error) {
	if policy == nil || policy.Raw == nil {
		return "", fmt.Errorf("policy is nil or has no raw data")
	}

	ordered := canonicalizeMap(policy.Raw)
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to encode canonical JSON: %w", err)
	}

	return string(data), nil
}

// orderedMap maintains insertion order for YAML encoding
type orderedMap struct {
	keys   []string
	values map[string]interface{}
}

func newOrderedMap() *orderedMap {
	return &orderedMap{
		keys:   make([]string, 0),
		values: make(map[string]interface{}),
	}
}

func (o *orderedMap) Set(key string, value interface{}) {
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *orderedMap) MarshalYAML() (interface{}, error) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}
	for _, k := range o.keys {
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: k,
		}
		var valueNode yaml.Node
		v := o.values[k]
		if err := valueNode.Encode(v); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, keyNode, &valueNode)
	}
	return node, nil
}

// canonicalizeMap recursively sorts map keys for canonical output.
func canonicalizeMap(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		// Get sorted keys
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		// For top-level policy, use preferred ordering
		if isTopLevel(keys) {
			keys = preferredKeyOrder(keys)
		}

		result := newOrderedMap()
		for _, k := range keys {
			result.Set(k, canonicalizeMap(val[k]))
		}
		return result

	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = canonicalizeMap(item)
		}
		return result

	default:
		return v
	}
}

// isTopLevel checks if these keys look like a top-level policy document.
func isTopLevel(keys []string) bool {
	for _, k := range keys {
		if k == "apiVersion" || k == "kind" || k == "metadata" || k == "spec" {
			return true
		}
	}
	return false
}

// preferredKeyOrder returns keys in preferred order for policy documents.
func preferredKeyOrder(keys []string) []string {
	// Define preferred order for common keys
	order := []string{"apiVersion", "kind", "metadata", "spec"}

	result := make([]string, 0, len(keys))
	seen := make(map[string]bool)

	// Add preferred keys first (in order)
	for _, preferred := range order {
		for _, k := range keys {
			if k == preferred {
				result = append(result, k)
				seen[k] = true
				break
			}
		}
	}

	// Add remaining keys alphabetically
	remaining := make([]string, 0)
	for _, k := range keys {
		if !seen[k] {
			remaining = append(remaining, k)
		}
	}
	sort.Strings(remaining)
	result = append(result, remaining...)

	return result
}

// DiffPolicies computes a simple diff between two policy renderings.
func DiffPolicies(local, remote string) string {
	if remote == "" {
		return "No remote policy to compare against."
	}

	localLines := strings.Split(local, "\n")
	remoteLines := strings.Split(remote, "\n")

	var diff strings.Builder
	diff.WriteString("--- remote (active)\n")
	diff.WriteString("+++ local\n")

	// Simple line-by-line diff
	maxLines := len(localLines)
	if len(remoteLines) > maxLines {
		maxLines = len(remoteLines)
	}

	inDiff := false
	contextLines := 3
	lastDiffLine := -contextLines - 1

	for i := 0; i < maxLines; i++ {
		localLine := ""
		remoteLine := ""
		if i < len(localLines) {
			localLine = localLines[i]
		}
		if i < len(remoteLines) {
			remoteLine = remoteLines[i]
		}

		if localLine != remoteLine {
			// Show context before diff
			if !inDiff {
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				if start > lastDiffLine+contextLines+1 {
					diff.WriteString(fmt.Sprintf("@@ line %d @@\n", i+1))
				}
				for j := start; j < i; j++ {
					if j < len(remoteLines) {
						diff.WriteString(fmt.Sprintf(" %s\n", remoteLines[j]))
					}
				}
			}
			inDiff = true
			lastDiffLine = i

			if remoteLine != "" {
				diff.WriteString(fmt.Sprintf("-%s\n", remoteLine))
			}
			if localLine != "" {
				diff.WriteString(fmt.Sprintf("+%s\n", localLine))
			}
		} else if inDiff {
			// Show context after diff
			if i <= lastDiffLine+contextLines {
				diff.WriteString(fmt.Sprintf(" %s\n", localLine))
			} else {
				inDiff = false
			}
		}
	}

	if diff.Len() == len("--- remote (active)\n+++ local\n") {
		return "No differences found."
	}

	return diff.String()
}

// ComparePolicies returns true if two policies are semantically equivalent.
func ComparePolicies(a, b *Policy) bool {
	if a == nil || b == nil {
		return a == b
	}

	canonA, err := CanonicalJSON(a)
	if err != nil {
		return false
	}

	canonB, err := CanonicalJSON(b)
	if err != nil {
		return false
	}

	return canonA == canonB
}
