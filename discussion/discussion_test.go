package discussion

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pmdb "aipmc/db"
)

func TestCJKBigrams(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"行为分析", []string{"行为", "为分", "分析"}},
		{"中文搜索召回", []string{"中文", "文搜", "搜索", "索召", "召回"}},
		{"a行为b", []string{"行为"}},
		{"搜索", []string{"搜索"}},
		{"单", nil},
		{"", nil},
		{"FTS5", nil},
	}
	for _, c := range cases {
		if got := cjkBigrams(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("cjkBigrams(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSnippetContent(t *testing.T) {
	long := "前面的上下文内容没有任何关系，真正命中的关键词行为分析出现在中间位置，后面的内容也是无关的补充说明文字。"
	got := SnippetContent(long, "行为分析", 8)
	if !strings.Contains(got, "行为分析") {
		t.Errorf("snippet must contain the hit, got %q", got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("mid-text hit should be elided on both sides, got %q", got)
	}
	if len([]rune(got)) > 60 {
		t.Errorf("snippet too long: %d runes, got %q", len([]rune(got)), got)
	}

	// CJK 2-gram fallback: row matched via bigrams, query not contiguous.
	nonContig := "讨论 agent 行为测量分析工具体系"
	got2 := SnippetContent(nonContig, "行为分析", 8)
	if !strings.Contains(got2, "行为") {
		t.Errorf("bigram fallback must show hit context, got %q", got2)
	}

	// No hit at all → falls back to head truncation.
	noHit := "完全没有关键词的内容，这里是完全不相关的一段文字，再补一点字数。"
	got3 := SnippetContent(noHit, "行为分析", 8)
	if !strings.HasPrefix(got3, "完全没有") {
		t.Errorf("no-hit fallback should show head, got %q", got3)
	}

	// Short content → returned as-is.
	short := "行为分析"
	if got4 := SnippetContent(short, "行为分析", 8); got4 != short {
		t.Errorf("short content should be unchanged, got %q", got4)
	}
}

func TestSearchCJKRecall(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".pmai", "data", "pmai.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	db, err := pmdb.OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	insert := `INSERT INTO discussion_log (id, session_id, role, source, content, created_at) VALUES (?,?,?,?,?,?)`
	rows := [][]any{
		{"r1", "s1", "user", "codex-cli", "讨论 agent 行为测量分析工具体系", "2026-08-14T10:00:00"},
		{"r2", "s1", "assistant", "codex-cli", "行为分析方案收敛", "2026-08-14T10:01:00"},
		{"r3", "s1", "user", "claude-code", "无关内容：FTS5 索引写入约束", "2026-08-14T10:02:00"},
	}
	for _, r := range rows {
		if _, err := db.Exec(insert, r...); err != nil {
			t.Fatal(err)
		}
	}

	// "行为分析" must recall the non-contiguous row r1 ("行为测量分析")
	// via 2-gram hits, and rank the exact-match row r2 first.
	results, total, err := Search(nil, "行为分析", "", "", "", dir, "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(results) == 0 {
		t.Fatal("no results returned")
	}
	if results[0]["id"] != "r2" {
		t.Errorf("exact match should rank first, got %v", results[0]["id"])
	}
	foundNonContiguous := false
	for _, r := range results {
		if r["id"] == "r1" {
			foundNonContiguous = true
		}
	}
	if !foundNonContiguous {
		t.Error("non-contiguous CJK match (行为测量分析) was not recalled")
	}

	// Plain 2-char query stays on the exact-substring path and still works.
	results2, total2, err := Search(nil, "索引", "", "", "", dir, "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total2 != 1 || results2[0]["id"] != "r3" {
		t.Errorf("2-char query: total=%d first=%v, want 1 / r3", total2, results2[0]["id"])
	}

	// Non-existent query returns zero.
	_, total3, err := Search(nil, "完全不存在的话题", "", "", "", dir, "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total3 != 0 {
		t.Errorf("no-match query: total = %d, want 0", total3)
	}

	// since filter: only rows with created_at >= since qualify.
	results4, total4, err := Search(nil, "行为分析", "", "", "", dir, "2026-08-14T10:01:00", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total4 != 1 || results4[0]["id"] != "r2" {
		t.Errorf("since filter: total=%d first=%v, want 1 / r2 (r1 is before the window)", total4, results4[0]["id"])
	}
}
