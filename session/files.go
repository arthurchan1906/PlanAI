package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"aipmc/db"
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
				paths := extractPathsFromCommand(md.Command)
				for _, p := range paths {
					touched[p] = true
				}
			case "read":
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

// knownProjectRoots is lazily loaded from the project registry on first use.
var (
	knownProjectRoots     []string
	knownProjectRootsOnce sync.Once
)

func getKnownProjectRoots() []string {
	knownProjectRootsOnce.Do(func() {
		projects := db.LoadCleanProjects()
		for _, p := range projects {
			if _, err := os.Stat(p.Path + "/.pmai"); err == nil {
				knownProjectRoots = append(knownProjectRoots, p.Path)
			}
		}
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			found := false
			for _, r := range knownProjectRoots {
				if r == cwd {
					found = true
					break
				}
			}
			if !found {
				knownProjectRoots = append(knownProjectRoots, cwd)
			}
		}
	})
	return knownProjectRoots
}

var fileExtRE = regexp.MustCompile(`\.(go|py|js|ts|jsx|tsx|swift|m|h|c|cpp|java|kt|rs|rb|sh|yaml|yml|json|sql|md|css|html|vue|svelte|toml|xml|plist|entitlements|pbxproj|xcscheme)$`)

func extractPathsFromCommand(cmd string) []string {
	if cmd == "" {
		return nil
	}

	var paths []string
	seen := map[string]bool{}

	tokens := splitShellTokens(cmd)
	for _, t := range tokens {
		if len(t) < 3 || t[0] == '-' {
			continue
		}

		for _, root := range getKnownProjectRoots() {
			if strings.HasPrefix(t, root+"/") {
				if fileExtRE.MatchString(t) {
					if !seen[t] {
						seen[t] = true
						paths = append(paths, t)
					}
				}
			}
		}

		if strings.Contains(t, "/") && fileExtRE.MatchString(t) && !strings.HasPrefix(t, "/") {
			if !seen[t] {
				seen[t] = true
				paths = append(paths, t)
			}
		}
	}

	for _, root := range getKnownProjectRoots() {
		base := filepath.Base(root)
		re := regexp.MustCompile(regexp.QuoteMeta("~"+root[strings.Index(root, base)-1:]) + `/[^\s"'\` + "`" + `;|&><]+`)
		matches := re.FindAllString(cmd, -1)
		homeDir, _ := os.UserHomeDir()
		for _, m := range matches {
			expanded := strings.Replace(m, "~", homeDir, 1)
			if fileExtRE.MatchString(expanded) && !seen[expanded] {
				seen[expanded] = true
				paths = append(paths, expanded)
			}
		}
	}

	return paths
}

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
