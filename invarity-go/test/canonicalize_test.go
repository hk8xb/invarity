package test

import (
	"testing"

	"invarity/internal/util"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},       // Not truncated
		{"hello world", 5, "he..."},  // Truncated with ellipsis
		{"hi", 2, "hi"},              // Not truncated
		{"hello", 3, "hel"},          // Truncated, no room for ellipsis
		{"", 10, ""},                 // Empty string
		{"a", 1, "a"},                // Not truncated
		{"ab", 1, "a"},               // Truncated, no room for ellipsis
	}

	for _, tt := range tests {
		result := util.TruncateString(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestCanonicalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected string
	}{
		{
			name:     "sorted keys",
			input:    map[string]any{"z": 1, "a": 2, "m": 3},
			expected: `{"a":2,"m":3,"z":1}`,
		},
		{
			name:     "nested sorted",
			input:    map[string]any{"b": map[string]any{"y": 1, "x": 2}},
			expected: `{"b":{"x":2,"y":1}}`,
		},
		{
			name:     "with array",
			input:    map[string]any{"items": []any{3, 1, 2}},
			expected: `{"items":[3,1,2]}`, // Arrays preserve order
		},
		{
			name:     "empty object",
			input:    map[string]any{},
			expected: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := util.CanonicalJSON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("got %s, want %s", string(result), tt.expected)
			}
		})
	}
}

func TestHashJSON(t *testing.T) {
	// Same content should produce same hash regardless of key order
	a := map[string]any{"x": 1, "y": 2}
	b := map[string]any{"y": 2, "x": 1}

	hashA, err := util.HashJSON(a)
	if err != nil {
		t.Fatalf("failed to hash a: %v", err)
	}

	hashB, err := util.HashJSON(b)
	if err != nil {
		t.Fatalf("failed to hash b: %v", err)
	}

	if hashA != hashB {
		t.Errorf("hashes should match: %s != %s", hashA, hashB)
	}

	// Different content should produce different hash
	c := map[string]any{"x": 1, "y": 3}
	hashC, _ := util.HashJSON(c)
	if hashA == hashC {
		t.Error("different content should produce different hash")
	}
}

func TestDedupeStrings(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			input:    []string{"x", "y", "z"},
			expected: []string{"x", "y", "z"},
		},
		{
			input:    []string{},
			expected: []string{},
		},
		{
			input:    []string{"a", "a", "a"},
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		result := util.DedupeStrings(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("got len %d, want %d", len(result), len(tt.expected))
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("at index %d: got %s, want %s", i, v, tt.expected[i])
			}
		}
	}
}

func TestStringSliceContains(t *testing.T) {
	slice := []string{"apple", "banana", "cherry"}

	if !util.StringSliceContains(slice, "banana") {
		t.Error("expected to find 'banana'")
	}

	if util.StringSliceContains(slice, "grape") {
		t.Error("should not find 'grape'")
	}

	if util.StringSliceContains([]string{}, "anything") {
		t.Error("empty slice should not contain anything")
	}
}

func TestMinMaxInt(t *testing.T) {
	if util.MinInt(5, 3) != 3 {
		t.Error("MinInt(5, 3) should be 3")
	}
	if util.MinInt(3, 5) != 3 {
		t.Error("MinInt(3, 5) should be 3")
	}
	if util.MaxInt(5, 3) != 5 {
		t.Error("MaxInt(5, 3) should be 5")
	}
	if util.MaxInt(3, 5) != 5 {
		t.Error("MaxInt(3, 5) should be 5")
	}
}
