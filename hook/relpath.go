package hook

import (
	"os"
	"path/filepath"
	"strings"
)

var projectRootCache string

// projectRoot returns the repo root for the current project — the directory
// containing the .pmai runtime dir, discovered by walking up from cwd (same
// walk as db.RuntimeDir). Falls back to cwd when no .pmai is found.
func projectRoot() string {
	if projectRootCache != "" {
		return projectRootCache
	}
	if dir := os.Getenv("PMAI_HOME"); dir != "" {
		projectRootCache = filepath.Dir(dir)
		return projectRootCache
	}
	if dir := os.Getenv("PLANAI_HOME"); dir != "" {
		projectRootCache = filepath.Dir(dir)
		return projectRootCache
	}
	cwd, _ := os.Getwd()
	for dir := cwd; dir != "/" && dir != "."; {
		pmaiDir := filepath.Join(dir, ".pmai")
		if info, err := os.Stat(pmaiDir); err == nil && info.IsDir() {
			projectRootCache = dir
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	projectRootCache = cwd
	return cwd
}

// ToRelPath normalizes a file path to repo-relative form (decision 1/2):
//   - absolute paths become relative to the project root;
//   - files outside the project root return "" (not recorded);
//   - already-relative paths are cleaned of ./ and ../ prefixes and kept.
func ToRelPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	root := projectRoot()
	if looksAbs(p) {
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
		return filepath.ToSlash(rel)
	}
	rel := filepath.ToSlash(filepath.Clean(p))
	rel = strings.TrimPrefix(rel, "./")
	for strings.HasPrefix(rel, "../") {
		rel = strings.TrimPrefix(rel, "../")
	}
	if rel == "." || rel == "" {
		return ""
	}
	return rel
}

// looksAbs reports whether p is absolute on this platform or uses a Windows
// drive-letter prefix (C:\... or C:/...), which filepath.IsAbs misses on macOS.
func looksAbs(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

// ExtractPatchFiles parses Codex apply_patch text (tool_input.patch or a Bash
// heredoc embedding it) and returns every `*** Update File:` / `*** Add File:`
// / `*** Delete File:` target, deduplicated in first-seen order. When the text
// contains a `*** Begin Patch` fence, only lines inside the fence are scanned;
// otherwise every marker line in the text is a candidate.
func ExtractPatchFiles(text string) []string {
	lines := strings.Split(text, "\n")
	fenced := strings.Contains(text, "*** Begin Patch")
	inPatch := false
	var files []string
	seen := map[string]bool{}
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "*** Begin Patch"):
			inPatch = true
			continue
		case strings.HasPrefix(trimmed, "*** End Patch"):
			inPatch = false
			continue
		}
		if fenced && !inPatch {
			continue
		}
		for _, marker := range []string{"*** Update File:", "*** Add File:", "*** Delete File:"} {
			if strings.HasPrefix(trimmed, marker) {
				fp := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
				if fp != "" && !seen[fp] {
					seen[fp] = true
					files = append(files, fp)
				}
				break
			}
		}
	}
	return files
}
