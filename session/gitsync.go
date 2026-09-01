package session

import (
	"encoding/json"
	"os/exec"
	"regexp"
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
		existingCommit := matchByHash(gc.hash)
		hasHash := existingCommit != nil

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

// gitCommitHeaderPattern 匹配 git log --format=%H|%s|%aI 的 commit header 行：
// 十六进制 hash 后跟 "|"（%H 恒为完整 hash，文件行不可能以此起头）。
// 捕获组 m[1]=hash、m[2]=首根 "|" 之后剩余（title|date）。
// 用 [0-9a-f]+ 而非写死 40：兼容 SHA-1（40 hex）与 SHA-256（64 hex）objectformat。
var gitCommitHeaderPattern = regexp.MustCompile(`^([0-9a-f]+)\|(.*)$`)

// parseGitLog parses "git log --format=%H|%s|%aI --name-only" output.
//
// 实际布局（8/31 实测）：每个 commit 块为
//
//	HEADER\n\nfile1\nfile2\n<NEXT_HEADER>   ← 文件列表后直接紧跟下一 header，无空行！
//
// 空行只出现在 header 与第一个文件之间。旧实现以「空行且 files>0」finalize，
// 会把下一个 header 行误 append 为上一 commit 的文件（污染 files_json，如
// "61d4355...|ci(release)...|2026-08-31T17:58:08+08:00" 整行混入），并使下一 commit
// 的文件行被当作 header 丢弃（只补一次后若该 commit 不在 DB 会被漏录）。
// 修复：以 header 行判定（40hex+pipe）作为块边界，遇 header 先 flush 当前块。
func parseGitLog(output string) []gitCommit {
	var commits []gitCommit
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var current *gitCommit

	flush := func() {
		if current != nil {
			commits = append(commits, *current)
			current = nil
		}
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if gitCommitHeaderPattern.MatchString(line) {
			// 新 commit header——闭合当前块（无论当前块有无文件）
			flush()
			m := gitCommitHeaderPattern.FindStringSubmatch(line)
			// m[1]=hash；m[2]="title|date"。%aI 是末段 ISO 日期（不含 "|"），
			// 标题 %s 却可能含 "|"——故从最右一个 "|" 切分，避免把标题后半段误当 date。
			hash := m[1]
			rest := m[2]
			title, date := rest, ""
			if i := strings.LastIndex(rest, "|"); i >= 0 {
				title = rest[:i]
				date = rest[i+1:]
			}
			current = &gitCommit{
				hash:  hash,
				title: title,
				date:  date,
			}
			continue
		}
		if current != nil {
			current.files = append(current.files, line)
		}
	}
	flush()

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
