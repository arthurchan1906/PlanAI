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
	if got := parseFeedbackRefs(`[{"type":"","id":"task:task-x"},{"type":"decision","id":"decision-x"}]`); len(got) != 1 {
		t.Fatalf("type 空的对象（回填合并的 L2 旧形态）应忽略, got %d", len(got))
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
