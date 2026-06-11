// Package u provides shared utility functions used across the project.
package u

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ── Time ──────────────────────────────────────────────────────────────

// NowISO returns the current time in ISO 8601 format (second precision).
func NowISO() string { return time.Now().Format("2006-01-02T15:04:05") }

// Today returns the current date as a string.
func Today() string { return time.Now().Format("2006-01-02") }

// ── ID generation ─────────────────────────────────────────────────────

// Slug generates a unique ID with the given prefix.
func Slug(prefix string) string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("%s-%s-%s", prefix, time.Now().Format("20060102-150405"), hex.EncodeToString(b))
}

// ── JSON ──────────────────────────────────────────────────────────────

// JsonStr marshals v to a JSON string, returning "" on error.
func JsonStr(v any) string { return string(jsonBytes(v)) }

// MustMarshal marshals v to JSON bytes, panicking on error.
func MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ParseJSON unmarshals a JSON string, returning def on error.
func ParseJSON(s string, def any) any {
	if s == "" {
		return def
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return def
	}
	return v
}

// ParseJSONList unmarshals a JSON array string, returning []any{} on error.
func ParseJSONList(s string) []any {
	v := ParseJSON(s, []any{})
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

// ParseJSONStrList unmarshals a JSON string array, returning []string{} on error.
func ParseJSONStrList(s string) []string {
	raw := ParseJSONList(s)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ── String helpers ────────────────────────────────────────────────────

// Str safely converts any to string, returning "" for nil or non-string values.
func Str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Itoa converts an int to a string without importing strconv.
func Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}

// TruncateStr truncates s to maxLen bytes.
func TruncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TruncateText truncates s to maxRunes runes.
func TruncateText(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// FirstNonEmpty returns the first non-empty string from candidates.
func FirstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

// SafePrefix returns the first n bytes of s.
func SafePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// SplitAndTrim splits s by sep and trims whitespace from each part, skipping empty parts.
func SplitAndTrim(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, sep)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if result == nil {
		result = []string{}
	}
	return result
}
