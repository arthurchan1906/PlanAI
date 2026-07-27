package session

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

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

		if meta == "" {
			continue
		}

		// Path A: Codex-style PostToolUse (role=assistant, type=new_file/edit/write/read)
		if role == "assistant" {
			var md struct {
				Type     string `json:"type"`
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(meta), &md); err == nil && md.FilePath != "" {
				switch md.Type {
				case "new_file", "edit", "write":
					touched[md.FilePath] = true
				case "read":
					read[md.FilePath] = true
				}
			}
		}

		// Path B: Claude-style tool messages (role=tool, type=bash/read/mcp_tool)
		if role == "tool" {
			var md struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Tool    string `json:"tool"`
			}
			if err := json.Unmarshal([]byte(meta), &md); err != nil {
				continue
			}

			switch md.Type {
			case "bash":
				// Extract file paths from bash commands (edit/write operations)
				paths := extractPathsFromCommand(md.Command)
				for _, p := range paths {
					touched[p] = true
				}
			case "read":
				// Claude Read tool — file_path may be in command field
				if md.Command != "" {
					paths := extractPathsFromCommand(md.Command)
					for _, p := range paths {
						read[p] = true
					}
				}
			}
		}
	}

	touchedFiles = mapKeys(touched)
	readFiles = mapKeys(read)
	return
}

// knownProjectRoots is used to filter extracted paths to only known project directories.
var knownProjectRoots = []string{
	"/Users/dazsec/workspace/aipmc",
	"/Users/dazsec/projects/EncryptDrive",
	"/Users/dazsec/projects/mac-dz",
}

// fileExtPattern matches common source code file extensions.
var fileExtRE = regexp.MustCompile(`\.(go|py|js|ts|jsx|tsx|swift|m|h|c|cpp|java|kt|rs|rb|sh|yaml|yml|json|sql|md|css|html|vue|svelte|toml|xml|plist|entitlements|pbxproj|xcscheme)$`)

// extractPathsFromCommand extracts file system paths from a bash command string.
// It looks for absolute paths under known project roots or relative paths with code extensions.
func extractPathsFromCommand(cmd string) []string {
	if cmd == "" {
		return nil
	}

	var paths []string
	seen := map[string]bool{}

	// Split command by whitespace and common shell operators
	tokens := splitShellTokens(cmd)
	for _, t := range tokens {
		// Skip short tokens and flags
		if len(t) < 3 || t[0] == '-' {
			continue
		}

		// Absolute paths under known project roots
		for _, root := range knownProjectRoots {
			if strings.HasPrefix(t, root+"/") {
				// Match file with extension, not just directories
				if fileExtRE.MatchString(t) {
					if !seen[t] {
						seen[t] = true
						paths = append(paths, t)
					}
				}
			}
		}

		// Relative paths that look like source files (contain / and end with extension)
		if strings.Contains(t, "/") && fileExtRE.MatchString(t) && !strings.HasPrefix(t, "/") {
			// Relative paths are project-relative; we store them as-is
			// They will match commit files similarly stored as relative paths
			if !seen[t] {
				seen[t] = true
				paths = append(paths, t)
			}
		}
	}

	// Also try to find paths in quoted strings and backtick expressions
	for _, root := range knownProjectRoots {
		base := filepath.Base(root)
		// Patterns like ~/workspace/aipmc/some/file.go
		re := regexp.MustCompile(regexp.QuoteMeta("~"+root[strings.Index(root, base)-1:]) + `/[^\s"'\` + "`" + `;|&><]+`)
		matches := re.FindAllString(cmd, -1)
		for _, m := range matches {
			// Expand ~ to home
			expanded := strings.Replace(m, "~", "/Users/dazsec", 1)
			if fileExtRE.MatchString(expanded) && !seen[expanded] {
				seen[expanded] = true
				paths = append(paths, expanded)
			}
		}
	}

	return paths
}

// splitShellTokens splits a shell command into tokens, handling quotes.
func splitShellTokens(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case (ch == ' ' || ch == ';' || ch == '|' || ch == '&' || ch == '>' || ch == '<') && !inSingle && !inDouble:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
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
