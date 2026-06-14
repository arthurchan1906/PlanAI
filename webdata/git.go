package webdata

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pmdb "aipmc/db"
)

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// CodePayload is returned by GET /pmai/web/code (git status; may be slow).
func CodePayload() map[string]any {
	return map[string]any{
		"code_status":        codeStatus(),
		"recent_git_commits": recentGitCommits(),
	}
}

func codeStatus() map[string]any {
	branch := runGit("rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		branch = "main"
	}
	cs := map[string]any{
		"branch": branch, "dirty": false,
		"staged": []any{}, "unstaged": []any{}, "untracked": []any{},
		"changed_files_count": 0,
	}
	statusOut := runGit("status", "--short")
	if statusOut != "" {
		staged, unstaged, untracked := []string{}, []string{}, []string{}
		for _, line := range strings.Split(statusOut, "\n") {
			if len(line) < 3 {
				continue
			}
			fp := strings.TrimSpace(line[3:])
			if strings.HasPrefix(fp, ".pmai/") {
				continue
			}
			idx, wt := line[0], line[1]
			if idx != ' ' && idx != '?' && !containsStr(staged, fp) {
				staged = append(staged, fp)
			}
			if wt != ' ' && wt != '?' && !containsStr(unstaged, fp) {
				unstaged = append(unstaged, fp)
			}
			if idx == '?' && wt == '?' && !containsStr(untracked, fp) {
				untracked = append(untracked, fp)
			}
		}
		cs["staged"] = staged
		cs["unstaged"] = unstaged
		cs["untracked"] = untracked
		cs["dirty"] = len(staged)+len(unstaged)+len(untracked) > 0
		cs["changed_files_count"] = len(staged) + len(unstaged) + len(untracked)
	}
	return cs
}

func recentGitCommits() []any {
	logOut := runGit("log", "-n10", "--date=iso-strict", "--name-only", "--pretty=format:%H%x1f%an%x1f%ad%x1f%s")
	if logOut == "" {
		return []any{}
	}
	var result []any
	var current map[string]any
	for _, line := range strings.Split(logOut, "\n") {
		if strings.Contains(line, "\x1f") {
			if current != nil {
				result = append(result, current)
			}
			parts := strings.SplitN(line, "\x1f", 4)
			if len(parts) >= 4 {
				current = map[string]any{
					"commit_hash": parts[0], "author": parts[1], "timestamp": parts[2],
					"title": parts[3], "files": []string{},
				}
			}
		} else if current != nil && strings.TrimSpace(line) != "" {
			fp := strings.TrimSpace(line)
			if !strings.HasPrefix(fp, ".pmai/") {
				files := current["files"].([]string)
				current["files"] = append(files, fp)
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	return result
}

func runGit(args ...string) string {
	d, err := pmdb.RuntimeDir()
	if err != nil {
		return ""
	}
	return runGitInDir(filepath.Dir(d), args...)
}

func runGitInDir(dir string, args ...string) string {
	if dir == "" {
		return ""
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[git] err=%v dir=%s args=%v\n", err, dir, args)
		return ""
	}
	return strings.TrimSpace(string(out))
}
