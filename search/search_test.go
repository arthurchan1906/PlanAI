package search

import (
	"os"
	"path/filepath"
	"testing"

	"aipmc/ai"
	pmdb "aipmc/db"
)

// Regression: RerankWithAI must re-rank the candidates the caller already
// fetched — it previously re-ran FTS5 inside the callback, ignoring the
// candidates argument and doubling the search work.
func TestRerankWithAIUsesProvidedCandidates(t *testing.T) {
	candidates := []Hit{
		{Type: "task", ID: "t1", Title: "候选一"},
		{Type: "task", ID: "t2", Title: "候选二"},
		{Type: "commit", ID: "c1", Title: "候选三"},
	}
	// Unconfigured client → Embed errors, HybridSearch falls back to the
	// recall candidates. The point is those candidates must be the ones the
	// caller provided (no re-run of FTS5 against the DB).
	got := RerankWithAI(&ai.Client{}, "anything", 2, candidates)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (candidates capped by limit)", len(got))
	}
	for i, h := range got {
		if h.ID != candidates[i].ID {
			t.Errorf("got[%d].ID = %q, want %q — candidates were ignored", i, h.ID, candidates[i].ID)
		}
	}
}

// CJK-aware matchScore: exact contiguous substring ranks above 2-gram-only
// recall, and ASCII terms keep their previous semantics.
func TestMatchScoreCJK(t *testing.T) {
	hay := "agent 行为测量分析工具体系"
	if s := matchScore(hay, []string{"行为测量分析"}); s <= 0 {
		t.Errorf("exact CJK substring must score > 0, got %d", s)
	}
	// "行为分析" is not contiguous in 行为测量分析, but 2 of its 2-grams are.
	if s := matchScore(hay, []string{"行为分析"}); s <= 0 {
		t.Errorf("CJK 2-gram overlap must score > 0, got %d", s)
	}
	if s := matchScore(hay, []string{"行为测量分析"}); s <= matchScore(hay, []string{"行为分析"}) {
		t.Errorf("exact match must outrank 2-gram-only: exact=%d gram=%d", s, matchScore(hay, []string{"行为分析"}))
	}
	// ASCII: unrelated terms stay 0, exact stays > 0.
	if s := matchScore("search behavior", []string{"behavior"}); s <= 0 {
		t.Errorf("ASCII exact must score > 0, got %d", s)
	}
	if s := matchScore("search behavior", []string{"encrypt"}); s != 0 {
		t.Errorf("ASCII non-match must score 0, got %d", s)
	}
}

// Entity search must recall CJK mid-string substring rows that FTS5 unicode61
// misses (the original Friday failure scenario), with exact hits first.
func TestEntitySearchCJKRecall(t *testing.T) {
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

	rows := [][]any{
		{"task", "t1", "Agent 行为测量分析工具体系", "讨论 agent 行为测量分析"},
		{"task", "t2", "行为分析方案收敛", "行为分析方案"},
		{"decision", "d1", "ED L2 摘要凭据库", "与中文无关内容"},
	}
	for _, r := range rows {
		if err := pmdb.SyncFTS5Entity(db, r[0].(string), r[1].(string), r[2].(string), r[3].(string)); err != nil {
			t.Fatal(err)
		}
	}

	// CJK mid-string substring query: FTS5 alone recalls only t2 (token
	// prefix), the supplement must add t1 via 2-gram overlap and rank t2 first.
	hits := FTS5WithDB(db, "行为分析", 10)
	got := map[string]Hit{}
	for _, h := range hits {
		got[h.ID] = h
	}
	if len(hits) == 0 || hits[0].ID != "t2" {
		t.Errorf("exact CJK row t2 must rank first, got %+v", hits)
	}
	if _, ok := got["t1"]; !ok {
		t.Errorf("2-gram recall: t1 (行为测量分析) must be present for query 行为分析, got %+v", hits)
	}
	if _, ok := got["d1"]; ok {
		t.Errorf("unrelated row d1 must not be recalled, got %+v", hits)
	}

	// ASCII query must keep working unchanged.
	ascii := FTS5WithDB(db, "L2 摘要", 10)
	seen := map[string]bool{}
	for _, h := range ascii {
		seen[h.ID] = true
	}
	if !seen["d1"] {
		t.Errorf("ASCII/L2 row d1 must be recalled, got %+v", ascii)
	}
	if seen["t1"] || seen["t2"] {
		t.Errorf("ASCII query must not leak CJK-only rows, got %+v", ascii)
	}
}
