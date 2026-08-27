package analyze

import (
	"strings"
	"testing"

	pmdb "aipmc/db"
	"aipmc/store"
)

func newIsolatedBriefDB(t *testing.T) {
	t.Helper()
	t.Setenv("PMAI_HOME", t.TempDir())
	if _, err := pmdb.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
}

// parseFeedbackRefs：对象形态（反馈）识别；[]string 形态（L2）忽略。
func TestParseFeedbackRefs(t *testing.T) {
	if got := parseFeedbackRefs(`[{"type":"decision","id":"decision-x","ref_text":"约束"}]`); len(got) != 1 {
		t.Fatalf("对象形态应识别 1 条, got %d", len(got))
	}
	if got := parseFeedbackRefs(`[{"type":"","id":"task:task-x"},{"type":"decision","id":"decision-x"}]`); len(got) != 0 {
		t.Fatalf("type 空或仅 id 的对象（L2 旧形态/引用计数）应忽略, got %d", len(got))
	}
	// L2 规范化对象（type 非空但无 ref_text/missing_queries）只是引用计数，
	// 不是"引用未查询"反馈，应忽略。
	if got := parseFeedbackRefs(`[{"type":"task","id":"task-x","ref_text":""},{"type":"decision","id":"decision-x","ref_text":""}]`); len(got) != 0 {
		t.Fatalf("无反馈特征的规范化 L2 对象应忽略, got %d", len(got))
	}
	// commit/task 高价值之外的类型即使带 ref_text 也不进反馈段
	if got := parseFeedbackRefs(`[{"type":"commit","id":"commit-x","ref_text":"git log"}]`); len(got) != 0 {
		t.Fatalf("commit 低价值不应进反馈段, got %d", len(got))
	}
	if got := parseFeedbackRefs(`["decision-x","task-y"]`); len(got) != 0 {
		t.Fatalf("L2 []string 形态应忽略, got %d", len(got))
	}
	if got := parseFeedbackRefs(""); got != nil {
		t.Fatalf("空串应返回 nil")
	}
}

// buildFeedbackBriefSection：写入反馈条目后，summary 段渲染 session + 实体。
func TestBuildFeedbackBriefSection(t *testing.T) {
	newIsolatedBriefDB(t)
	if err := store.UpsertSessionSummary(store.SessionSummary{
		SessionID:  "S1",
		Source:     "codex-cli",
		EntityRefs: `[{"type":"decision","id":"decision-x","ref_text":"约束","missing_queries":["aipm_get_decision"]}]`,
	}); err != nil {
		t.Fatal(err)
	}
	compact := buildFeedbackBriefSection(true)
	if !strings.Contains(compact, "引用未查询反馈") {
		t.Fatalf("summary 段缺标题: %s", compact)
	}
	if !strings.Contains(compact, "S1") || !strings.Contains(compact, "decision-x") {
		t.Fatalf("summary 段缺 session/实体: %s", compact)
	}
	full := buildFeedbackBriefSection(false)
	if !strings.Contains(full, "查询确认") {
		t.Fatalf("full 段缺建议提示: %s", full)
	}
	// L2 []string 形态不应触发反馈段
	if err := store.UpsertSessionSummary(store.SessionSummary{
		SessionID:  "S2",
		Source:     "claude-code",
		EntityRefs: `["decision-y"]`,
	}); err != nil {
		t.Fatal(err)
	}
	if got := buildFeedbackBriefSection(true); strings.Contains(got, "S2") {
		t.Fatalf("L2 []string 形态不应进反馈段: %s", got)
	}
}
