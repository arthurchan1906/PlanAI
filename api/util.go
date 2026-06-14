package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pmdb "aipmc/db"
)

func readBody(r *http.Request) map[string]any {
	var body map[string]any
	b, _ := io.ReadAll(r.Body)
	if len(b) > 0 {
		json.Unmarshal(b, &body)
	}
	if body == nil {
		body = map[string]any{}
	}
	return body
}

func parseEntityID(path string) (entity, id string) {
	parts := strings.SplitN(path, "/", 2)
	entity = parts[0]
	if len(parts) > 1 {
		id = parts[1]
	}
	return
}

func extractID(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	return s
}

func pstr(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func runGit(args ...string) string {
	d, err := pmdb.RuntimeDir()
	if err != nil {
		return ""
	}
	projectRoot := filepath.Dir(d)
	cmd := exec.Command("git", args...)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[git] err=%v dir=%s args=%v\n", err, projectRoot, args)
		return ""
	}
	return strings.TrimSpace(string(out))
}

func apiPath(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/pmai")
	return strings.TrimPrefix(path, "/")
}
