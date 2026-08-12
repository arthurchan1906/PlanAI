package session

import (
	"encoding/json"
	"os/exec"
	"strings"

	"aipmc/store"
	"aipmc/u"
)

// syncCommitsFromGit scans git log and backfills missing commit data.
// First run: full backfill of all commits with empty files_json.
// Subsequent runs: incremental (only recent commits from git).
// projectPath overrides cwd for multi-project scanning.
func syncCommitsFromGit(projectPath string, fullBackfill bool) (created, updated int) {
	// Build git log command — run inside the project dir (cmd.Dir, no global
	// CWD change). Store calls below pass projectPath explicitly.
	args := []string{"log", "--all", "--format=%H|%s|%aI", "--since=30.days"}
	args = append(args, "--name-only")

	cmd := exec.Command("git", args...)
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		u.LogShared("GITSYNC", "git log error: %v", err)
		return
	}

	parsed := parseGitLog(string(out))
	if len(parsed) == 0 {
		return
	}

	// Get all existing commits from DB to find matches
	existing, _ := store.ListCommitsFor(projectPath, "", "", "", "", 0)
	// Build prefix-based hash index — DB hashes may be short (7-8 chars)
	// while git log produces full 40-char hashes
	dbHashes := make([]struct{ hash, id string }, 0)
	for _, c := range existing {
		if h := u.Str(c["commit_hash"]); h != "" {
			dbHashes = append(dbHashes, struct{ hash, id string }{h, u.Str(c["id"])})
		}
	}

	// Match git hash against DB hash prefix
	matchByHash := func(gitHash string) map[string]any {
		for _, h := range dbHashes {
			if strings.HasPrefix(gitHash, h.hash) {
				for _, c := range existing {
					if u.Str(c["id"]) == h.id {
						return c
					}
				}
			}
		}
		return nil
	}

	for _, gc := range parsed {
		existingCommit := matchByHash(gc.hash); hasHash := existingCommit != nil

		if hasHash {
			// Existing commit — check if files need backfill
			filesStr := u.Str(existingCommit["files_json"])
			if filesStr == "" || filesStr == "[]" || filesStr == "null" {
				filesJSON, _ := json.Marshal(gc.files)
				if _, err := store.UpdateCommitFor(projectPath, u.Str(existingCommit["id"]), map[string]any{
					"files":       string(filesJSON),
					"commit_hash": gc.hash,
				}); err == nil {
					updated++
				}
			}
		} else {
			// New commit — create from git data (no task association)
			if _, err := store.StoreGitCommit(
				projectPath,
				gc.title,
				gc.hash,
				gc.date,
				gc.files,
			); err == nil {
				created++
			}
		}
	}

	// Update files_json for commits matched by title (no hash)
	// Only on first full backfill
	if fullBackfill {
		titleIndex := map[string][]map[string]any{}
		for _, c := range existing {
			filesStr := u.Str(c["files_json"])
			if filesStr != "" && filesStr != "[]" && filesStr != "null" {
				continue // already has files
			}
			t := u.Str(c["title"])
			titleIndex[t] = append(titleIndex[t], c)
		}

		for _, gc := range parsed {
			if matchByHash(gc.hash) != nil {
				continue // already handled above
			}
			if candidates, ok := titleIndex[gc.title]; ok {
				for _, c := range candidates {
					filesJSON, _ := json.Marshal(gc.files)
					if _, err := store.UpdateCommitFor(projectPath, u.Str(c["id"]), map[string]any{
						"files":       string(filesJSON),
						"commit_hash": gc.hash,
					}); err == nil {
						updated++
					}
				}
			}
		}
	}

	u.LogShared("GITSYNC", "project=%s full=%v created=%d updated=%d total_parsed=%d",
		projectPath, fullBackfill, created, updated, len(parsed))

	return
}

type gitCommit struct {
	hash  string
	title string
	date  string
	files []string
}

// parseGitLog parses "git log --format=%H|%s|%aI --name-only" output.
func parseGitLog(output string) []gitCommit {
	var commits []gitCommit
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var current *gitCommit

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			// Only finalize when we already have files — empty lines
			// between header and first file are separators, not delimiters.
			if current != nil && len(current.files) > 0 {
				commits = append(commits, *current)
				current = nil
			}
			continue
		}

		if current == nil {
			// New commit header: "hash|title|date"
			parts := strings.SplitN(line, "|", 3)
			if len(parts) < 2 {
				continue
			}
			current = &gitCommit{
				hash:  parts[0],
				title: parts[1],
			}
			if len(parts) >= 3 {
				current.date = parts[2]
			}
		} else {
			// File path
			if !strings.HasPrefix(line, " ") && line != "" {
				current.files = append(current.files, line)
			}
		}
	}
	// Don't forget the last one
	if current != nil {
		commits = append(commits, *current)
	}

	// Deduplicate files per commit
	for i := range commits {
		seen := map[string]bool{}
		var unique []string
		for _, f := range commits[i].files {
			if !seen[f] {
				seen[f] = true
				unique = append(unique, f)
			}
		}
		commits[i].files = unique
	}

	return commits
}
