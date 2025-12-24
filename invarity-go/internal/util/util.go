// Package util provides utility functions for the Invarity Firewall.
package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// CanonicalJSON returns a canonical (deterministic) JSON representation.
// Keys are sorted alphabetically at all levels.
func CanonicalJSON(v any) ([]byte, error) {
	// First marshal to JSON, then unmarshal to interface{} to normalize
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}

	return canonicalMarshal(normalized)
}

func canonicalMarshal(v any) ([]byte, error) {
	switch val := v.(type) {
	case map[string]any:
		return canonicalMarshalMap(val)
	case []any:
		return canonicalMarshalSlice(val)
	default:
		return json.Marshal(v)
	}
}

func canonicalMarshalMap(m map[string]any) ([]byte, error) {
	// Get sorted keys
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build canonical object
	result := []byte("{")
	for i, k := range keys {
		if i > 0 {
			result = append(result, ',')
		}
		keyBytes, _ := json.Marshal(k)
		result = append(result, keyBytes...)
		result = append(result, ':')

		valBytes, err := canonicalMarshal(m[k])
		if err != nil {
			return nil, err
		}
		result = append(result, valBytes...)
	}
	result = append(result, '}')
	return result, nil
}

func canonicalMarshalSlice(s []any) ([]byte, error) {
	result := []byte("[")
	for i, item := range s {
		if i > 0 {
			result = append(result, ',')
		}
		itemBytes, err := canonicalMarshal(item)
		if err != nil {
			return nil, err
		}
		result = append(result, itemBytes...)
	}
	result = append(result, ']')
	return result, nil
}

// HashJSON computes SHA256 hash of canonical JSON representation.
func HashJSON(v any) (string, error) {
	canonical, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

// HashBytes computes SHA256 hash of raw bytes.
func HashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// TruncateString truncates a string to maxLen characters, adding "..." if truncated.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// TruncateStringSlice truncates each string in a slice.
func TruncateStringSlice(ss []string, maxLen int) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = TruncateString(s, maxLen)
	}
	return result
}

// NowUTC returns the current time in UTC.
func NowUTC() time.Time {
	return time.Now().UTC()
}

// TimeSince returns duration since t in milliseconds.
func TimeSinceMs(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}

// MeasureDuration returns a function that, when called, returns the duration since start.
func MeasureDuration() func() time.Duration {
	start := time.Now()
	return func() time.Duration {
		return time.Since(start)
	}
}

// MinInt returns the minimum of two integers.
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MaxInt returns the maximum of two integers.
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// StringSliceContains checks if a slice contains a string.
func StringSliceContains(ss []string, s string) bool {
	for _, item := range ss {
		if item == s {
			return true
		}
	}
	return false
}

// DedupeStrings removes duplicates from a string slice while preserving order.
func DedupeStrings(ss []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// SafeJSONRawMessage safely handles potentially nil or empty json.RawMessage.
func SafeJSONRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// PtrTo returns a pointer to the given value.
func PtrTo[T any](v T) *T {
	return &v
}
