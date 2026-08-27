package eval

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFeedbackTestDB 内存 SQLite + discussion_log 表（与生产 schema 对齐检测所需列）。
func newFeedbackTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// 共享缓存内存库：多连接共享同一内存库（DetectFeedbackGaps 内部多次查询，
	// 与插入行需同库可见；:memory: 默认每连接独立库会丢数据）。
	db, err := sql.Open("sqlite", "file:fb_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE discussion_log (
		id TEXT, session_id TEXT, role TEXT, source TEXT,
		content TEXT, metadata TEXT, created_at TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	return db
}

func insertRow(t *testing.T, db *sql.DB, id, sid, role, source, content, createdAt string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO discussion_log VALUES (?,?,?,?,?,?,?)`,
		id, sid, role, source, content, "", createdAt); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

// 强漏查：引用 decision 但 session 无任何查询调用 → MissingQueries 输出全部期望工具。
func TestFeedbackDetectStrongMiss(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S1", "assistant", "codex", "参考 decision-20260826-172138-fb48b1（约束 A）处理", "2026-08-27T10:00:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(gaps))
	}
	g := gaps[0]
	if len(g.EntityRefs) != 1 || g.EntityRefs[0].Type != "decision" {
		t.Fatalf("EntityRefs = %+v, want 1 decision ref", g.EntityRefs)
	}
	want := []string{"aipm_get_decision", "aipm_list_decisions", "aipm_search_context", "aipm_smart_search"}
	if !equalStrings(g.MissingQueries, want) {
		t.Errorf("MissingQueries = %v, want %v", g.MissingQueries, want)
	}
}

// 有查询调用 → 保守不判漏查（工具行无参数，无法验证具体 ID）。
func TestFeedbackDetectQueriedNoMiss(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S2", "assistant", "codex", "参考 decision-20260826-172138-fb48b1 处理", "2026-08-27T10:00:00")
	insertRow(t, db, "t1", "S2", "tool", "codex", "🛠 mcp__aipm__aipm_get_decision", "2026-08-27T10:01:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(gaps))
	}
	if len(gaps[0].MissingQueries) != 0 {
		t.Errorf("MissingQueries = %v, want empty (已调用 get_decision)", gaps[0].MissingQueries)
	}
}

// 数据源规范性：无来源词 → has_source=false；[MCP] 标注 → true。
func TestFeedbackDataSourceRefs(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S3", "assistant", "codex",
		"基线实测决策查询 8/26=7 次，总调用量 220 次", "2026-08-27T10:00:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(gaps))
	}
	refs := gaps[0].DataSourceRefs
	if len(refs) != 2 {
		t.Fatalf("DataSourceRefs = %d, want 2", len(refs))
	}
	// "7 次" 前 40 字符内含 "实测" → has_source=true；"220 次" 前是 "总量"，无来源词 → false
	got := map[string]bool{}
	for _, r := range refs {
		got[r.Claim] = r.HasSource
	}
	if !got["7 次"] {
		t.Errorf("7 次 has_source = false, want true（附近有'实测'）")
	}
	if got["220 次"] {
		t.Errorf("220 次 has_source = true, want false（无来源词）")
	}
}

// user 消息引用不算 agent 行为；session 无 assistant 不输出。
func TestFeedbackUserRefIgnored(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "u1", "S4", "user", "human", "看看 decision-20260826-172138-fb48b1", "2026-08-27T10:00:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 0 {
		t.Errorf("gaps = %d, want 0（user 引用不检）", len(gaps))
	}
}

// 多类型引用 + 部分查询：decision 漏查、task 已查 → 只报 decision。
func TestFeedbackMultiTypePartial(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S5", "assistant", "codex",
		"引用 decision-20260826-172138-fb48b1 和 task-20260827-111103-939d0f", "2026-08-27T10:00:00")
	insertRow(t, db, "t1", "S5", "tool", "codex", "🛠 mcp__aipm__aipm_get_task", "2026-08-27T10:01:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(gaps))
	}
	g := gaps[0]
	for _, mq := range g.MissingQueries {
		if strings.HasPrefix(mq, "aipm_get_task") || strings.HasPrefix(mq, "aipm_list_tasks") {
			t.Errorf("MissingQueries 含 task 工具 %s（task 已查）", mq)
		}
	}
	foundDecision := false
	for _, mq := range g.MissingQueries {
		if strings.HasPrefix(mq, "aipm_get_decision") {
			foundDecision = true
		}
	}
	if !foundDecision {
		t.Errorf("MissingQueries 缺 decision 工具: %v", g.MissingQueries)
	}
	// 去重：公共工具（search_context）跨类型只出现一次
	seen := map[string]bool{}
	for _, mq := range g.MissingQueries {
		if seen[mq] {
			t.Errorf("MissingQueries 含重复 %s: %v", mq, g.MissingQueries)
		}
		seen[mq] = true
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 审核回归（8/27 Claude Challenge 1）：📡 工具结果摘要行（hook 写入 role=assistant）
// 中 task-xxx 是工具参数，不是 agent 正文引用——不得提取、不得判漏查。
func TestFeedbackSkipsToolSummaryLine(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S6", "assistant", "codex", "📡 aipm_update_task_status ✅ task-20260615-172610-6ccede →done", "2026-08-27T10:00:00")
	insertRow(t, db, "m2", "S6", "assistant", "codex", "📡 aipm_read_discussions ✅ last_n=8", "2026-08-27T10:01:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 0 {
		t.Errorf("gaps = %d, want 0（📡 摘要行不产生实体引用）", len(gaps))
	}
}

// 审核回归（Claude Challenge 2）：session 最近活跃但历史消息（since 之前）不得被扫。
func TestFeedbackSinceScopedToWindow(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S7", "assistant", "codex", "老消息引用 decision-20260616-000000-000000", "2026-06-16T10:36:40")
	insertRow(t, db, "m2", "S7", "assistant", "codex", "新消息无引用", "2026-08-27T10:00:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-25T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 0 {
		t.Errorf("gaps = %d, want 0（6 月消息在窗口外不得检出）", len(gaps))
	}
}

// 审核回归（Claude Challenge 3）：unknown session 不得选中（会把全部历史合成一个假 session）。
func TestFeedbackFiltersUnknownSession(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "unknown", "assistant", "codex", "引用 decision-20260616-000000-000000", "2026-08-27T10:00:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 0 {
		t.Errorf("gaps = %d, want 0（unknown session 过滤）", len(gaps))
	}
}

// 审核回归（Claude Challenge 5）：真实 task 工具参数场景——📡 行含 task-xxx +
// 正文含 task 引用但 session 已 update 过该 task → 不得判 task 漏查（工具参数非正文）。
func TestFeedbackTaskParamNotMiss(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S8", "assistant", "codex", "📡 aipm_update_task_status ✅ task-20260615-172610-6ccede →done", "2026-08-27T10:00:00")
	insertRow(t, db, "m2", "S8", "assistant", "codex", "正文讨论 task-20260615-172610-6ccede 的进度", "2026-08-27T10:02:00")

	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1（正文引用存在）", len(gaps))
	}
	// task 类型：工具参数行已被跳过，但正文引用仍在 → 判 task 漏查合理；
	// 关键断言是不得因 📡 参数行产生额外引用。
	for _, r := range gaps[0].EntityRefs {
		if strings.HasPrefix(r.Context, "📡") {
			t.Errorf("EntityRef 来自 📡 摘要行: %+v", r)
		}
	}
}

// ---- P3 T2 shadow 接线测试 ----

// 契约变换：FeedbackGap → C2 契约（entity_refs{type,id,ref_text} + data_sources + agent）。
func TestFeedbackShadowContractTransform(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S1", "assistant", "codex",
		"参考 decision-20260826-172138-fb48b1 处理，实测 3 次均失败", "2026-08-27T10:00:00")
	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil || len(gaps) != 1 {
		t.Fatalf("Detect: gaps=%d err=%v, want 1", len(gaps), err)
	}
	path := filepath.Join(t.TempDir(), "shadow.jsonl")
	written, skipped, err := WriteFeedbackShadow(path, gaps)
	if err != nil || written != 1 || skipped != 0 {
		t.Fatalf("Write: written=%d skipped=%d err=%v", written, skipped, err)
	}
	b, _ := os.ReadFile(path)
	var e FeedbackShadowEntry
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("unmarshal shadow: %v (raw=%s)", err, b)
	}
	if e.SessionID != "S1" || e.Agent != "codex" || e.Timestamp != "2026-08-27T10:00:00" {
		t.Errorf("head fields = %+v", e)
	}
	if len(e.EntityRefs) != 1 || e.EntityRefs[0].Type != "decision" ||
		e.EntityRefs[0].ID != "decision-20260826-172138-fb48b1" {
		t.Errorf("EntityRefs = %+v, want 1 decision ref with ref_text", e.EntityRefs)
	}
	if e.EntityRefs[0].RefText == "" {
		t.Errorf("RefText empty, want 引用行")
	}
	if len(e.DataSources) != 1 || e.DataSources[0] != "实测" {
		t.Errorf("DataSources = %v, want [实测]", e.DataSources)
	}
	if len(e.DataSourceRefs) != 1 || !e.DataSourceRefs[0].HasSource {
		t.Errorf("DataSourceRefs = %+v, want 1 claim with source", e.DataSourceRefs)
	}
}

// 去重幂等：同 session+timestamp 重跑 → skipped，不产生重复行。
func TestFeedbackShadowDedupAppend(t *testing.T) {
	db := newFeedbackTestDB(t)
	defer db.Close()
	insertRow(t, db, "m1", "S1", "assistant", "claude",
		"decision-20260826-172138-fb48b1 约束 A", "2026-08-27T10:00:00")
	gaps, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil || len(gaps) != 1 {
		t.Fatalf("Detect: %d %v", len(gaps), err)
	}
	path := filepath.Join(t.TempDir(), "shadow.jsonl")
	// 首次写
	if w, s, err := WriteFeedbackShadow(path, gaps); err != nil || w != 1 || s != 0 {
		t.Fatalf("first write w=%d s=%d err=%v", w, s, err)
	}
	// 重跑同一批 → 全 skipped
	if w, s, err := WriteFeedbackShadow(path, gaps); err != nil || w != 0 || s != 1 {
		t.Fatalf("rerun w=%d s=%d err=%v", w, s, err)
	}
	// 新增 session → append 1 行
	insertRow(t, db, "m2", "S2", "assistant", "codex",
		"task-20260827-111105-3f6872 引用", "2026-08-27T11:00:00")
	gaps2, err := DetectFeedbackGaps(db, "2026-08-01T00:00:00", 10)
	if err != nil {
		t.Fatalf("Detect2: %v", err)
	}
	if w, s, err := WriteFeedbackShadow(path, gaps2); err != nil || w != 1 || s != 1 {
		t.Fatalf("append w=%d s=%d err=%v", w, s, err)
	}
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (dedup)", len(lines))
	}
}
