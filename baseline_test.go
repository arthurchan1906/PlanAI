package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanLLMLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aipmc.log")
	log := `[2026-08-17 23:59:59] [LLM] agent=codex session=a in_tok=500 out_tok=50 cache_hit=400 injected=N lat=1.0s
[2026-08-18 00:00:01] [LLM] agent=codex session=a in_tok=600 out_tok=40 cache_hit=500 injected=N lat=1.2s
[2026-08-18 00:00:02] [LLM] agent=codex session=b in_tok=700 out_tok=30 injected=N lat=0.9s
[2026-08-18 00:00:03] [LLM] agent=codex session= in_tok=1 out_tok=2 cache_hit=0 injected=N lat=0.0s
[2026-08-18 00:00:04] [LLM] agent=codex session= in_tok=1 out_tok=2 cache_hit=0 injected=Y lat=0.0s
[2026-08-18 00:00:05] [LLM] agent=claude session= model=x in_tok=300 out_tok=20 cache_hit=100 injected=N lat=2.0s
[2026-08-18 00:00:06] [LLM] agent=codex model=x in_tok=800 out_tok=60 injected=N lat=1.5s
[15:00:01] [LLM] agent=codex session=old in_tok=900 out_tok=70 injected=N lat=1.0s
[2026-08-18 00:00:07] [MCP] tool=search_discussions status=OK
`
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 8, 18, 0, 0, 0, 0, time.Local)
	coverage, sessions, hours, err := scanLLMLines(path, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours["codex"]) != 1 || !hours["codex"]["2026-08-18T00"] {
		t.Errorf("codex hour activity = %v, want 1 distinct hour 2026-08-18T00（测试日志全在同一小时）", hours["codex"])
	}

	cx := coverage["codex"]
	if cx == nil {
		t.Fatal("codex coverage missing")
	}
	if cx.TotalLines != 5 {
		t.Errorf("codex total = %d, want 5（窗口内 codex 5 行：2 带 session + 2 探针 + 1 无 session 字段）", cx.TotalLines)
	}
	if cx.WithSession != 2 {
		t.Errorf("codex with_session = %d, want 2", cx.WithSession)
	}
	if cx.EmptySession != 3 {
		t.Errorf("codex empty_session = %d, want 3（00:00:03/04 探针 + 00:00:06 无 session 行）", cx.EmptySession)
	}
	if cx.ProbeLines != 2 {
		t.Errorf("codex probe = %d, want 2", cx.ProbeLines)
	}
	if sessions["codex"]["a"] != 1 || sessions["codex"]["b"] != 1 {
		t.Errorf("codex sessions = %v, want a=1 b=1", sessions["codex"])
	}

	cl := coverage["claude"]
	if cl == nil || cl.TotalLines != 1 || cl.WithSession != 0 || cl.EmptySession != 1 {
		t.Errorf("claude coverage = %+v, want 1 line, session empty", cl)
	}
}

func TestBuildUnderreport(t *testing.T) {
	llm := map[string]map[string]int{
		"codex": {"a": 5, "b": 3, "c": 1},
	}
	disc := map[string]map[string]int{
		"codex": {"a": 10, "b": 8},
	}
	coverage := map[string]*coverageStat{
		"codex":  {TotalLines: 4, WithSession: 3, EmptySession: 1},
		"claude": {TotalLines: 2, WithSession: 0, EmptySession: 2},
	}
	out := buildUnderreport(llm, disc, coverage)

	codex := out["codex"]
	if codex.Status != "ok" || codex.SessionsWithLLM != 3 || codex.SessionsMissing != 1 || codex.MissingRate != 1.0/3.0 {
		t.Errorf("codex underreport = %+v, want missing c only (1/3)", codex)
	}
	if len(codex.MissingSessionIDs) != 1 || codex.MissingSessionIDs[0] != "c" {
		t.Errorf("missing ids = %v, want [c]", codex.MissingSessionIDs)
	}
	claude := out["claude"]
	if claude.Status != "unmeasurable" || claude.Reason == "" {
		t.Errorf("claude underreport = %+v, want unmeasurable with reason", claude)
	}
}

func TestBuildOrphans(t *testing.T) {
	disc := map[string]map[string]int{
		"codex":  {"a": 10, "b": 8, "d": 3},
		"claude": {"x": 5},
	}
	llm := map[string]map[string]int{
		"codex": {"a": 5, "b": 3},
	}
	out := buildOrphans(disc, llm)

	codex := out["codex"]
	if codex.Status != "ok" || codex.SessionsWithDiscussion != 3 || codex.SessionsMissingInLLM != 1 {
		t.Errorf("codex orphans = %+v, want d missing (1/3)", codex)
	}
	if len(codex.MissingSessionIDs) != 1 || codex.MissingSessionIDs[0] != "d" {
		t.Errorf("missing ids = %v, want [d]", codex.MissingSessionIDs)
	}
	claude := out["claude"]
	if claude.Status != "unmeasurable" || claude.Reason == "" || claude.SessionsWithDiscussion != 1 {
		t.Errorf("claude orphans = %+v, want unmeasurable", claude)
	}
}

func TestParseBaselineSince(t *testing.T) {
	ts, err := parseBaselineSince("2026-08-17")
	if err != nil || ts.Format("2006-01-02") != "2026-08-17" {
		t.Errorf("date-only parse failed: %v %v", ts, err)
	}
	ts, err = parseBaselineSince("2026-08-17T10:00:00")
	if err != nil || ts.Hour() != 10 {
		t.Errorf("ISO parse failed: %v %v", ts, err)
	}
	if _, err := parseBaselineSince("not-a-date"); err == nil {
		t.Error("invalid since should error")
	}
}

func TestBuildCoarseAlignment(t *testing.T) {
	llm := map[string]map[string]bool{
		"claude": {"2026-08-17T09": true, "2026-08-17T10": true, "2026-08-17T12": true},
		"codex":  {"2026-08-17T09": true},
	}
	disc := map[string]map[string]bool{
		"claude": {"2026-08-17T09": true, "2026-08-17T11": true},
		"cursor": {"2026-08-17T09": true},
	}
	out := buildCoarseAlignment(llm, disc)

	cl := out["claude"]
	if cl.Status != "ok" || cl.HoursWithLLM != 3 || cl.HoursWithDisc != 2 || cl.HoursBoth != 1 || cl.HoursLLMOnly != 2 || cl.HoursDiscOnly != 1 {
		t.Errorf("claude coarse = %+v, want llm=3 disc=2 both=1 llm_only=2 disc_only=1", cl)
	}
	cursor := out["cursor"]
	if cursor.Status != "unmeasurable" || cursor.Reason == "" {
		t.Errorf("cursor coarse = %+v, want unmeasurable（有 discussion 但日志无 [LLM] 行）", cursor)
	}
	codex := out["codex"]
	if codex.Status != "ok" || codex.HoursLLMOnly != 1 || codex.HoursBoth != 0 {
		t.Errorf("codex coarse = %+v, want llm_only=1 both=0（disc 侧无 codex 数据）", codex)
	}
}

func TestScanDiscussionDBs(t *testing.T) {
	dir := t.TempDir()
	dbA := filepath.Join(dir, "projA", ".pmai", "data", "pmai.db")
	dbB := filepath.Join(dir, "projB", ".pmai", "data", "pmai.db")
	os.MkdirAll(filepath.Dir(dbA), 0o755)
	os.MkdirAll(filepath.Dir(dbB), 0o755)

	makeDB := func(path string, rows [][6]string) {
		d, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		_, err = d.Exec(`CREATE TABLE discussion_log (
			id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, created_at TEXT NOT NULL,
			embedding_json TEXT DEFAULT '', metadata TEXT DEFAULT '', thread_id TEXT DEFAULT '')`)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			_, err := d.Exec(`INSERT INTO discussion_log (id, session_id, role, source, content, created_at)
				VALUES (?,?,?,?,?,?)`, r[0], r[1], r[2], r[3], r[4], r[5])
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	// 同一 session 跨两个项目库（bug-141137 场景：断言被跨项目聚合，不误判为漏录）。
	makeDB(dbA, [][6]string{
		{"d1", "sess-a", "user", "claude-code", "hi", "2026-08-29T10:00:00"},
		{"d2", "sess-b", "assistant", "codex-cli", "ok", "2026-08-29T10:00:00"},
	})
	makeDB(dbB, [][6]string{
		{"d3", "sess-a", "tool", "claude-code", "op", "2026-08-29T11:00:00"},
		{"d4", "sess-b", "user", "codex-cli", "x", "2026-08-29T10:00:00"},
	})

	missing := filepath.Join(dir, "missing", ".pmai", "data", "pmai.db")
	disc, hours, scanned := scanDiscussionDBs([]string{dbA, dbB, missing}, "2026-08-29T00:00:00")

	if len(scanned) != 2 {
		t.Fatalf("scanned = %d, want 2 (missing DB 应跳过), got %v", len(scanned), scanned)
	}
	// claude sess-a 跨两库聚合 = 2，codex sess-b = 2。
	if got := disc["claude"]["sess-a"]; got != 2 {
		t.Fatalf("claude sess-a count = %d, want 2（跨项目聚合）", got)
	}
	if got := disc["codex"]["sess-b"]; got != 2 {
		t.Fatalf("codex sess-b count = %d, want 2", got)
	}
	// 小时聚合：claude 同时有 10 点与 11 点。
	if !hours["claude"]["2026-08-29T10"] || !hours["claude"]["2026-08-29T11"] {
		t.Fatalf("claude hours 应含 10/11 两小时, got %v", hours["claude"])
	}
}
