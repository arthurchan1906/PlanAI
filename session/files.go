package session

import (
	"encoding/json"

	"aipmc/u"
)

// classifyFiles extracts touched (Write/Edit) and read file paths from
// PostToolUse metadata in session messages. Shared by L2 summary and L3 reconcile.
func classifyFiles(messages []map[string]any) (touchedFiles, readFiles []string) {
	touched := map[string]bool{}
	read := map[string]bool{}

	for _, m := range messages {
		role := u.Str(m["role"])
		meta := u.Str(m["metadata"])

		// Only process assistant messages with metadata
		if role != "assistant" || meta == "" {
			continue
		}

		var md struct {
			Type     string `json:"type"`
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal([]byte(meta), &md); err != nil || md.FilePath == "" {
			continue
		}

		switch md.Type {
		case "new_file", "edit", "write":
			touched[md.FilePath] = true
		case "read":
			read[md.FilePath] = true
		}
	}

	touchedFiles = mapKeys(touched)
	readFiles = mapKeys(read)
	return
}

func mapKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
