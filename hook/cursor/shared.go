package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	pmdb "aipmc/db"
	"aipmc/u"
)

func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

func truncateText(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	count := 0
	for i := range s {
		if count >= maxRunes {
			return s[:i] + "..."
		}
		count++
	}
	return s
}

func parseToolInput(raw json.RawMessage) map[string]string {
	result := make(map[string]string)
	if len(raw) == 0 {
		return result
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return result
	}
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[k] = FixHookText(s)
		}
	}
	return result
}

func buildFullMeta(eventType string, rawJSON []byte) string {
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		// Cursor on Windows may produce JSON with invalid UTF-8 bytes in
		// Chinese text. Try sanitizing: replace invalid UTF-8 sequences
		// with the Unicode replacement character and re-parse.
		sanitized := strings.ToValidUTF8(string(rawJSON), "�")
		if err2 := json.Unmarshal([]byte(sanitized), &raw); err2 != nil {
			// Still failed. Build a minimal metadata with just the event type
			// and the raw payload so the entry is at least classified.
			raw = map[string]any{
				"_type":       eventType,
				"hook_event_name": eventType,
				"raw":         string(rawJSON),
			}
			b, _ := json.Marshal(raw)
			return string(b)
		}
	}
	raw["_type"] = eventType
	// Preserve transcript_path in metadata for potential text recovery.
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}

func dumpRawHook(platform, timestamp string, data []byte) {
	logDir := hookLogDir()
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, platform+"-hook.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "=== [%s] len=%d ===\n", timestamp, len(data))
	f.Write(data)
	f.WriteString("\n=== END ===\n\n")
}

// AppendHookDiagnostic writes a one-line note to the hook log (always on).
// Used for skips and DB failures that would otherwise only appear in debug stderr.
func AppendHookDiagnostic(platform, timestamp, message string) {
	if message == "" {
		return
	}
	logDir := hookLogDir()
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, platform+"-hook.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "--- [%s] %s ---\n", timestamp, message)
}

func hookLogDir() string {
	dir, err := pmdb.RuntimeDir()
	if err == nil && dir != "" {
		return filepath.Join(dir, "logs")
	}
	return filepath.Join(os.TempDir(), "aipm-hooks")
}

func shellQuote(s string) string {
	if strings.Contains(s, " ") {
		return `"` + s + `"`
	}
	return s
}

// PatchHunk is a unified-diff hunk for frontend rendering.
type PatchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

func parseDiffToHunks(diff string) []PatchHunk {
	var hunks []PatchHunk
	lines := strings.Split(diff, "\n")
	var currentHunk *PatchHunk

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
			}
			parts := strings.Split(line, " ")
			if len(parts) >= 3 {
				oldPart := strings.TrimPrefix(parts[1], "-")
				newPart := strings.TrimPrefix(parts[2], "+")
				oldNums := strings.Split(oldPart, ",")
				newNums := strings.Split(newPart, ",")
				h := PatchHunk{}
				if _, err := fmt.Sscanf(oldNums[0], "%d", &h.OldStart); err != nil {
					currentHunk = nil
					continue
				}
				if len(oldNums) > 1 {
					fmt.Sscanf(oldNums[1], "%d", &h.OldLines)
				} else {
					h.OldLines = 1
				}
				if _, err := fmt.Sscanf(newNums[0], "%d", &h.NewStart); err != nil {
					currentHunk = nil
					continue
				}
				if len(newNums) > 1 {
					fmt.Sscanf(newNums[1], "%d", &h.NewLines)
				} else {
					h.NewLines = 1
				}
				currentHunk = &h
			}
		} else if currentHunk != nil {
			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") {
				currentHunk.Lines = append(currentHunk.Lines, line)
			}
		}
	}
	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}
	return hunks
}

func isNewFile(toolResp json.RawMessage) bool {
	if len(toolResp) == 0 {
		return false
	}
	var resp struct {
		ReturnDisplay struct {
			IsNewFile bool `json:"isNewFile"`
		} `json:"returnDisplay"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		return resp.ReturnDisplay.IsNewFile
	}
	var flat struct {
		IsNewFile bool `json:"isNewFile"`
	}
	if json.Unmarshal(toolResp, &flat) == nil {
		return flat.IsNewFile
	}
	return false
}

func extractExitCode(toolResp json.RawMessage) int {
	if len(toolResp) == 0 {
		return 0
	}
	var flat struct {
		ExitCode int `json:"exitCode"`
	}
	if json.Unmarshal(toolResp, &flat) == nil && flat.ExitCode != 0 {
		return flat.ExitCode
	}
	var nested struct {
		ReturnDisplay struct {
			ExitCode int `json:"exitCode"`
		} `json:"returnDisplay"`
	}
	if json.Unmarshal(toolResp, &nested) == nil && nested.ReturnDisplay.ExitCode != 0 {
		return nested.ReturnDisplay.ExitCode
	}
	return 0
}

func fmtExitCode(code int) string {
	if code == 0 {
		return ""
	}
	return " [exit:" + u.Itoa(code) + "]"
}

func uitoa(n int) string {
	return u.Itoa(n)
}
