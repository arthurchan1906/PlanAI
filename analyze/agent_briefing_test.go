package analyze

import (
	"strings"
	"testing"

	pmdb "aipmc/db"
)

// newIsolatedBriefDBAgent creates an isolated in-memory PM DB for agent_briefing tests.
func newIsolatedBriefDBAgent(t *testing.T) {
	t.Helper()
	t.Setenv("PMAI_HOME", t.TempDir())
	if _, err := pmdb.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
}

// P0 ④a：agent_briefing 上下文卡必须携带当前进行中任务的确定性关联——
// task×file（graph_edges file_touch→commit→task）与 task×decision
// （related_decisions_json）。零语义，全确定性。
func TestBuildAgentBriefingDeterministicAssoc(t *testing.T) {
	newIsolatedBriefDBAgent(t)
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// task-1 in_progress，关联两个决策（一个存在、一个缺失）
	if _, err := d.Exec(`INSERT INTO tasks (id, title, status, priority, phase, acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at, roadmap_id, plan_id, created_at) VALUES ('task-1', '修 proxy 段注入', 'in_progress', 'P0', 'agent', '[]', '[]', '["decision-1","decision-2"]', '', '2026-01-01', '', 'plan-1', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO decisions (id, title, date, status, background, decision_text, impact_json, alternatives_json, related_tasks_json, updates_canon) VALUES ('decision-1', '确定性关联优先', '2026-01-01', 'accepted', 'b', 'txt', '[]', '[]', '["task-1"]', 0)`); err != nil {
		t.Fatal(err)
	}
	// commit 关联 task-1，file_touch 边指向该 commit 的 intersect 文件
	if _, err := d.Exec(`INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at) VALUES ('commit-1', 'c', '', '', '', 'main', 'h1', 'task-1', '', 'committed', 'passed', 'approved', '[]', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO graph_edges (id, source_type, source_id, edge_type, target_type, target_id, weight, evidence_json, created_at) VALUES ('gedge-1', 'session', 'sess-1', 'file_touch', 'commit', 'commit-1', 1.0, '{"intersect":["mcp/mcp.go","store/store.go"]}', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}

	card1 := BuildAgentBriefing()
	if card1 == "" {
		t.Fatal("应返回非空上下文卡")
	}
	// task×file：确定性关联（graph_edges→commit→task）
	for _, want := range []string{"task-1", "mcp/mcp.go", "store/store.go", "修 proxy 段注入", "P0", "in_progress"} {
		if !strings.Contains(card1, want) {
			t.Fatalf("上下文卡缺少 %q，card=%q", want, card1)
		}
	}
	// task×decision：存在决策显示 accepted+title，不存在显示 unknown 而非丢弃
	if !strings.Contains(card1, "decision-1 (accepted) 《确定性关联优先》") {
		t.Fatalf("上下文卡未含决策链，card=%q", card1)
	}
	if !strings.Contains(card1, "decision-2 (unknown)") {
		t.Fatalf("上下文卡未能呈现缺失决策为 unknown（不应丢弃），card=%q", card1)
	}

	// 确定性：两次调用输出一致（排序稳定）
	if card2 := BuildAgentBriefing(); card2 != card1 {
		t.Fatalf("上下文卡非确定性：card1=%q card2=%q", card1, card2)
	}
}

// 无进行中任务时上下文卡应为空（调用方跳过，不占注入预算）。
func TestBuildAgentBriefingNoInProgress(t *testing.T) {
	newIsolatedBriefDBAgent(t)
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO tasks (id, title, status, priority, phase, acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at, roadmap_id, plan_id, created_at) VALUES ('task-2', 'done task', 'done', 'P1', 'general', '[]', '[]', '[]', '', '2026-01-01', '', '', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if got := BuildAgentBriefing(); got != "" {
		t.Fatalf("无进行中任务应返回空，got %q", got)
	}
}
