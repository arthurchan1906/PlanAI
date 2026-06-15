package collab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/u"
)

const discussionAlertPrefix = "⚠️ [讨论模式告警]"

// DiscussionModeFlagDir returns the directory for discussion-mode flag files.
func DiscussionModeFlagDir() string {
	dir, err := pmdb.RuntimeDir()
	if err != nil {
		return ""
	}
	cacheDir := filepath.Join(dir, "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	return cacheDir
}

// SetDiscussionMode creates or removes the flag file that Hooks check to
// enforce discussion-mode write alerts.
func SetDiscussionMode(topicID string, on bool) error {
	dir := DiscussionModeFlagDir()
	if dir == "" {
		return nil
	}
	flagPath := filepath.Join(dir, "discussion-mode-"+topicID+".json")
	if on {
		info := map[string]any{"topic_id": topicID, "started_at": u.NowISO()}
		data, _ := json.Marshal(info)
		return os.WriteFile(flagPath, data, 0644)
	}
	_ = os.Remove(flagPath)
	return nil
}

// ActiveDiscussionTopics returns topic IDs that have discussion-mode enforcement active.
func ActiveDiscussionTopics() []string {
	dir := DiscussionModeFlagDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var topics []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "discussion-mode-") && strings.HasSuffix(e.Name(), ".json") {
			topicID := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "discussion-mode-"), ".json")
			topics = append(topics, topicID)
		}
	}
	return topics
}

// MaybeAlertDiscussionWrite logs an alert when a file write occurs in discussion mode.
func MaybeAlertDiscussionWrite(sessionID, source, toolName, filePath string) {
	if filePath == "" || sessionID == "" {
		return
	}
	if !isFileWriteTool(toolName) {
		return
	}
	if !IsDiscussionModeSession() {
		return
	}
	if IsDiscussionWhitelistedPath(filePath) {
		return
	}

	meta := u.JsonStr(map[string]any{
		"type":      "discussion_mode_alert",
		"file_path": filePath,
		"tool":      toolName,
	})
	msg := fmt.Sprintf("%s %s 在讨论模式下 %s 非白名单路径: %s",
		discussionAlertPrefix, source, toolName, filePath)
	_, _ = store.LogDiscussion(sessionID, "assistant", source, msg, meta)
}

// MaybeAlertFromToolInput extracts a file path from tool JSON and checks discussion mode.
func MaybeAlertFromToolInput(sessionID, source, toolName string, toolInput json.RawMessage) {
	if len(toolInput) == 0 {
		return
	}
	MaybeAlertDiscussionWrite(sessionID, source, toolName, ExtractToolFilePath(toolInput))
}

// IsDiscussionModeSession checks whether any active topic has discussion-mode
// enforcement on, OR the env var AIPM_DISCUSSION_MODE=1 is set.
func IsDiscussionModeSession() bool {
	if os.Getenv("AIPM_DISCUSSION_MODE") == "1" {
		return true
	}
	return len(ActiveDiscussionTopics()) > 0
}

// IsDiscussionWhitelistedPath returns true for paths allowed during discussion mode.
func IsDiscussionWhitelistedPath(path string) bool {
	if path == "" {
		return true
	}
	norm := filepath.ToSlash(strings.ToLower(path))
	if strings.Contains(norm, "/.pmai/") || strings.HasPrefix(norm, ".pmai/") {
		return true
	}
	if strings.HasSuffix(norm, ".lock") || strings.Contains(norm, "/node_modules/") {
		return true
	}
	return false
}

// ExtractToolFilePath reads common path keys from tool_input JSON.
func ExtractToolFilePath(toolInput json.RawMessage) string {
	var raw map[string]any
	if err := json.Unmarshal(toolInput, &raw); err != nil {
		return ""
	}
	for _, key := range []string{"file_path", "filePath", "path", "file"} {
		if v, ok := raw[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func isFileWriteTool(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	name = strings.TrimPrefix(name, "mcp__")
	switch name {
	case "write", "edit", "strreplace", "str_replace", "write_file", "replace", "edit_file", "apply_patch":
		return true
	}
	return strings.HasSuffix(name, "_write") ||
		strings.HasSuffix(name, "_write_file") ||
		strings.HasSuffix(name, "_edit")
}
