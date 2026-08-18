package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pmdb "aipmc/db"
)

func setupAnalyzeDB(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", "pmai.db"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", home)
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
}

func seedPlanTask(t *testing.T, scope string) {
	t.Helper()
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO plans (id, roadmap_id, vision_id, title, goal, status, priority, scope_json, risks_json, assumptions_json, task_ids_json, source, created_at, updated_at) VALUES ('plan-1', '', '', 'P', 'G', 'active', 'P1', ?, '[]', '[]', '[]', 'test', '2026-01-01', '2026-01-01')`, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tasks (id, title, status, priority, phase, acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at, roadmap_id, plan_id, created_at) VALUES ('task-1', 'T', 'in_progress', 'P1', 'general', '[]', '[]', '[]', '', '2026-01-01', '', 'plan-1', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
}

func seedCommit(t *testing.T, id, created, files string) {
	t.Helper()
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at) VALUES (?, 'c', '', '', '', 'main', ?, 'task-1', '', 'committed', 'passed', 'approved', ?, ?, ?)`, id, "hash-"+id, files, created, created); err != nil {
		t.Fatal(err)
	}
}

// Regression: AnalyzeScopeDrift used to scan the full commit history
// (ListCommits limit=0); a large repo produced hundreds of drift entries and a
// ~180KB briefing. It must be bounded to recent commits.
func TestScopeDriftBoundedToRecentCommits(t *testing.T) {
	setupAnalyzeDB(t)
	seedPlanTask(t, `["视频预览"]`)
	for i := 0; i < 55; i++ {
		created := fmt.Sprintf("2026-01-01T00:%02d:%02d", i/60, i%60)
		seedCommit(t, fmt.Sprintf("commit-%03d", i), created, `["Unmatched/File.swift"]`)
	}
	results := AnalyzeScopeDrift()
	if len(results) > scopeDriftCommitLimit {
		t.Fatalf("AnalyzeScopeDrift returned %d results, want ≤ %d", len(results), scopeDriftCommitLimit)
	}
	if len(results) == 0 {
		t.Fatal("expected some drift results")
	}
}

// Regression: a single unmatched file used to flag the whole commit
// (scope-keyword mismatch noise). Only a strict majority out of scope counts.
func TestScopeDriftRequiresMajorityOutOfScope(t *testing.T) {
	setupAnalyzeDB(t)
	seedPlanTask(t, `["视频预览"]`)
	// 9/10 files in scope, 1 out → not drift
	seedCommit(t, "commit-minor", "2026-01-01T00:00:00", `["视频预览/A.swift","视频预览/B.swift","视频预览/C.swift","视频预览/D.swift","视频预览/E.swift","视频预览/F.swift","视频预览/G.swift","视频预览/H.swift","视频预览/I.swift","Other/J.swift"]`)
	// 2/10 in scope, 8 out → drift
	seedCommit(t, "commit-major", "2026-01-02T00:00:00", `["视频预览/A.swift","视频预览/B.swift","Other/C.swift","Other/D.swift","Other/E.swift","Other/F.swift","Other/G.swift","Other/H.swift","Other/I.swift","Other/J.swift"]`)

	results := AnalyzeScopeDrift()
	var minor, major bool
	for _, r := range results {
		if r.CommitID == "commit-minor" {
			minor = true
		}
		if r.CommitID == "commit-major" {
			major = true
		}
	}
	if minor {
		t.Error("commit with 1/10 files out of scope must not be flagged")
	}
	if !major {
		t.Error("commit with 8/10 files out of scope must be flagged")
	}
}

func TestBuildBriefingLevelSummaryCompact(t *testing.T) {
	setupAnalyzeDB(t)
	// 有数据场景：summary（计数级）应显著小于 full（完整明细）。
	seedPlanTask(t, `{"plan-1": ["file_a.go"]}`)
	seedCommit(t, "c1", "2026-08-01T00:00:00", `["file_a.go"]`)
	sum, _ := BuildBriefingLevel(nil, "", "summary")
	full, _ := BuildBriefingLevel(nil, "", "full")
	if sum == "" || full == "" {
		t.Fatalf("both levels should render: sum=%d chars full=%d chars", len(sum), len(full))
	}
	if len([]rune(sum)) >= len([]rune(full)) {
		t.Errorf("有数据时 summary (%d) 应小于 full (%d)\nsummary:\n%s\nfull:\n%s", len([]rune(sum)), len([]rune(full)), sum, full)
	}
	if !strings.Contains(sum, "level=full") {
		t.Errorf("summary 应提示 full 展开入口，got:\n%s", sum)
	}
	if !strings.Contains(sum, "进行中的任务") {
		t.Errorf("summary 应含任务清单，got:\n%s", sum)
	}
}

func TestBuildBriefingSummaryEmptyDBCompact(t *testing.T) {
	setupAnalyzeDB(t)
	// 空库 summary 应远小于 20KB 目标（结构上最多计数级 + 少量标题）。
	sum, _ := BuildBriefingLevel(nil, "", "summary")
	if n := len([]rune(sum)); n > 2048 {
		t.Errorf("空库 summary 应 ≤2KB，got %d 字符:\n%s", n, sum)
	}
	if !strings.Contains(sum, "🏗️ 项目简报 — AIPM（summary）") {
		t.Errorf("summary 应有标题标记，got:\n%s", sum)
	}
}
