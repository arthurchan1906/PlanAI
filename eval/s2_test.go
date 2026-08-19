package eval

// EVAL_PIPELINE S2 测试：阶段1 回合化 + 阶段2 意图分类 + 阶段3 段切分。
// LLM 通道用替身注入，覆盖兜底规则/低置信回退/混合信号边界。

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildTurns(t *testing.T) {
	d := fixtureDB(t)
	ins := func(id, role, content, ts, md string) {
		mustExec(t, d, `INSERT INTO discussion_log (id, session_id, role, source, content, created_at, metadata) VALUES (?,?,?,?,?,?,?)`,
			id, "s1", role, "codex-cli", content, ts, md)
	}
	ins("d1", "user", "重构 parse", "2026-08-14T10:00:00", "")
	ins("d2", "assistant", "🔧 ls", "2026-08-14T10:00:05", `{"_type":"post_tool","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	ins("d3", "tool", "stdout", "2026-08-14T10:00:06", "")
	ins("d4", "user", "继续", "2026-08-14T10:01:00", "")
	ins("d5", "assistant", "🔧 Read a.go", "2026-08-14T10:01:05", `{"_type":"post_tool","file_path":"a.go","tool_name":"Read","tool_input":{"file_path":"a.go"}}`)

	turns, err := BuildTurns(d, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(turns))
	}
	if turns[0].UserMsg != "重构 parse" {
		t.Errorf("turn0 user = %q", turns[0].UserMsg)
	}
	if len(turns[0].Records) != 2 {
		t.Errorf("turn0 records = %d, want 2", len(turns[0].Records))
	}
	if turns[0].Records[0].Tool.Tool != "bash" {
		t.Errorf("turn0 rec0 tool = %q, want bash", turns[0].Records[0].Tool.Tool)
	}
	if turns[1].Records[0].Tool.Tool != "read" {
		t.Errorf("turn1 rec0 tool = %q, want read", turns[1].Records[0].Tool.Tool)
	}
	files := turns[1].Files()
	if len(files) != 1 || files[0] != "a.go" {
		t.Errorf("turn1 files = %v, want [a.go]", files)
	}
}

type stubClassifier struct {
	c   IntentClass
	err error
}

func (s stubClassifier) Classify(string) (IntentClass, error) { return s.c, s.err }

func TestRuleBasedIntent(t *testing.T) {
	if c, ok := ruleBasedIntent("继续"); !ok || c.Type != IntentDialogue {
		t.Errorf("继续 → %v/%v, want dialogue", c, ok)
	}
	if c, ok := ruleBasedIntent("暂时不要开工"); !ok || c.Type != IntentDialogue {
		t.Errorf("暂时不要开工 → %v/%v, want dialogue", c, ok)
	}
	if c, ok := ruleBasedIntent("Push"); !ok || c.Type != IntentDialogue {
		t.Errorf("Push → %v/%v, want dialogue（大小写不敏感）", c, ok)
	}
	if _, ok := ruleBasedIntent("你是谁"); ok {
		t.Error("无关键词短句不应兜底")
	}
	if _, ok := ruleBasedIntent("重构整个模块并补充测试"); ok {
		t.Error("长句不应兜底")
	}
}

func TestClassifyIntent(t *testing.T) {
	if c := ClassifyIntent("继续", nil); c.Type != IntentDialogue {
		t.Errorf("无 LLM 短句 = %s, want dialogue", c.Type)
	}
	if c := ClassifyIntent("重构整个模块并补充测试", stubClassifier{IntentClass{IntentTask, 0.9}, nil}); c.Type != IntentTask {
		t.Errorf("LLM 高置信 = %s, want task", c.Type)
	}
	if c := ClassifyIntent("重构整个模块并补充测试", nil); c.Type != IntentTask || c.Confidence != 0.5 {
		t.Errorf("无 LLM 长句 = %v, want task/0.5（降级保边界）", c)
	}
	if c := ClassifyIntent("重构整个模块并补充测试", stubClassifier{IntentClass{IntentTask, 0.5}, nil}); c.Type != IntentTask {
		t.Errorf("LLM 低置信回退 = %s, want task（兜底过滤器后判 task）", c.Type)
	}
	if c := ClassifyIntent("你是谁", stubClassifier{IntentClass{}, errors.New("llm down")}); c.Type != IntentTask {
		t.Errorf("LLM 错误 = %s, want task（降级保边界）", c.Type)
	}
}

func TestSegmentEpisodes(t *testing.T) {
	base := time.Date(2026, 8, 14, 10, 0, 0, 0, time.Local)
	mk := func(msg string, start time.Time, files ...string) Turn {
		turn := Turn{UserMsg: msg, Start: start, End: start.Add(2 * time.Minute)}
		for _, f := range files {
			turn.Records = append(turn.Records, Record{Content: f, Tool: ToolRecord{Tool: "edit", Files: []string{f}}})
		}
		return turn
	}
	sid := "019f0230-016b-7db0-bcd5-098380db3e11"
	turns := []Turn{
		mk("重构 parse", base),            // 10:00 任务型开段
		mk("继续", base.Add(5*time.Minute), "a.go"),  // 10:05 并入（文件 a.go）
		mk("看下效果", base.Add(10*time.Minute), "b.go"), // 10:10 并入（文件突变 b.go）
		mk("查状态", base.Add(97*time.Minute)),       // 11:37 距 commit >60min → 弱边界
	}
	classes := []IntentClass{
		{Type: IntentTask, Confidence: 0.9},
		{Type: IntentDialogue, Confidence: 0.8},
		{Type: IntentDialogue, Confidence: 0.8},
		{Type: IntentDialogue, Confidence: 0.8},
	}
	commits := []CommitInfo{
		{Hash: "c0", CreatedAt: base.Add(-30 * time.Minute)}, // 段开始前：不归属
		{Hash: "c1", CreatedAt: base.Add(7 * time.Minute)},   // 10:07 段1内
	}
	eps := SegmentEpisodes(sid, "codex-cli", turns, classes, commits, DefaultSegParams())
	if len(eps) != 2 {
		t.Fatalf("episodes = %d, want 2", len(eps))
	}
	if eps[0].Boundary != "forced_by_task" {
		t.Errorf("ep0 boundary = %q, want forced_by_task", eps[0].Boundary)
	}
	if !strings.HasPrefix(eps[0].ID, "ep-019f0230-") {
		t.Errorf("ep0 id = %q", eps[0].ID)
	}
	if len(eps[0].Turns) != 3 {
		t.Errorf("ep0 turns = %d, want 3", len(eps[0].Turns))
	}
	if !eps[0].JaccardHit {
		t.Error("ep0 应标记 Jaccard 突变佐证（a.go→b.go）")
	}
	if len(eps[0].Commits) != 1 || eps[0].Commits[0] != "c1" {
		t.Errorf("ep0 commits = %v, want [c1]", eps[0].Commits)
	}
	if len(eps[0].Files) != 2 {
		t.Errorf("ep0 files = %v, want [a.go b.go]", eps[0].Files)
	}
	if eps[1].Boundary != "commit_gap" {
		t.Errorf("ep1 boundary = %q, want commit_gap", eps[1].Boundary)
	}
	if eps[1].IntentText != "查状态" {
		t.Errorf("ep1 intent = %q, want 查状态", eps[1].IntentText)
	}
	if len(eps[1].Turns) != 1 {
		t.Errorf("ep1 turns = %d, want 1", len(eps[1].Turns))
	}
}

func TestJaccard(t *testing.T) {
	if j := jaccard(nil, nil); j != 1 {
		t.Errorf("空集 = %v, want 1", j)
	}
	if j := jaccard([]string{"a"}, []string{"a"}); j != 1 {
		t.Errorf("同集 = %v, want 1", j)
	}
	if j := jaccard([]string{"a"}, []string{"b"}); j != 0 {
		t.Errorf("异集 = %v, want 0", j)
	}
	if j := jaccard([]string{"a", "b"}, []string{"b", "c"}); j != 1.0/3.0 {
		t.Errorf("半交 = %v, want 1/3", j)
	}
}
