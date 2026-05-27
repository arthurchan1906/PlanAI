package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func nowISO() string { return time.Now().Format("2006-01-02T15:04:05") }
func today() string  { return time.Now().Format("2006-01-02") }

func slug(prefix string) string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("%s-%s-%s", prefix, time.Now().Format("20060102-150405"), hex.EncodeToString(b))
}

func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func jsonStr(v any) string { return string(jsonBytes(v)) }

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func parseJSON(s string, def any) any {
	if s == "" {
		return def
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return def
	}
	return v
}

func parseJSONList(s string) []any {
	v := parseJSON(s, []any{})
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

func parseJSONStrList(s string) []string {
	raw := parseJSONList(s)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
