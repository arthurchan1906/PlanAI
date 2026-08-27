package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pmdb "aipmc/db"
	"aipmc/store"
)

// newIsolatedDB 在 PMAI_HOME 指向的临时目录 bootstrap 一个独立库，
// 隔离 store 写入（避免污染真实 cwd 项目库）。
func newIsolatedDB(t *testing.T) {
	t.Helper()
	t.Setenv("PMAI_HOME", t.TempDir())
	if _, err := pmdb.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
}

func writeShadow(t *testing.T, dir string, entries ...FeedbackShadowEntry) string {
	t.Helper()
	path := filepath.Join(dir, "shadow.jsonl")
	var b []byte
	for _, e := range entries {
		line, _ := json.Marshal(e)
		b = append(b, line...)
		b = append(b, '\n')
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 高价值过滤 + 回填：decision/bug 强漏查进反馈；commit 引用（低价值）被过滤。
func TestBackfillHighValueFilter(t *testing.T) {
	newIsolatedDB(t)
	shadow := writeShadow(t, t.TempDir(), FeedbackShadowEntry{
		SessionID: "S1", Agent: "codex-cli", Timestamp: "2026-08-27T10:00:00",
		EntityRefs: []ShadowEntityRef{
			{Type: "decision", ID: "decision-20260826-172138-fb48b1", RefText: "约束 A"},
			{Type: "commit", ID: "commit-20260811-163518-e786ef", RefText: "git log 引用"},
			{Type: "bug", ID: "bug-20260805-134225-4f214f", RefText: "锁竞争"},
		},
		MissingQueries: []string{"aipm_get_decision", "aipm_get_bug", "aipm_get_commit"},
	})
	sessions, refs, err := BackfillFeedback(shadow)
	if err != nil || sessions != 1 {
		t.Fatalf("backfill: sessions=%d err=%v, want 1", sessions, err)
	}
	if refs != 2 {
		t.Fatalf("refs=%d, want 2 (decision+bug 进反馈，commit 被过滤)", refs)
	}
	row, err := store.GetSessionSummary("S1")
	if err != nil || row == nil {
		t.Fatalf("GetSessionSummary: %v", err)
	}
	var got []FeedbackBackfillRef
	if err := json.Unmarshal([]byte(row.EntityRefs), &got); err != nil {
		t.Fatalf("unmarshal entity_refs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entity_refs=%d entries, want 2", len(got))
	}
	for _, r := range got {
		if r.Type == "commit" {
			t.Fatalf("commit 低价值不应进反馈: %+v", r)
		}
		if len(r.MissingQueries) == 0 {
			t.Fatalf("ref %s 缺 missing_queries", r.ID)
		}
	}
	if row.QualityScore != 0 {
		t.Fatalf("quality_score=%d, want 0（反馈不覆盖工作流分数）", row.QualityScore)
	}
}

// 幂等合并：同 session 二次回填按 id 去重。
func TestBackfillIdempotentMerge(t *testing.T) {
	newIsolatedDB(t)
	entry := FeedbackShadowEntry{
		SessionID: "S1", Agent: "codex-cli", Timestamp: "2026-08-27T10:00:00",
		EntityRefs:     []ShadowEntityRef{{Type: "decision", ID: "decision-x", RefText: "约束"}},
		MissingQueries: []string{"aipm_get_decision"},
	}
	shadow := writeShadow(t, t.TempDir(), entry)
	if _, _, err := BackfillFeedback(shadow); err != nil {
		t.Fatal(err)
	}
	sessions, refs, err := BackfillFeedback(shadow)
	if err != nil || sessions != 0 || refs != 0 {
		t.Fatalf("second run sessions=%d refs=%d err=%v, want 0/0 (id 去重)", sessions, refs, err)
	}
	row, _ := store.GetSessionSummary("S1")
	var got []FeedbackBackfillRef
	json.Unmarshal([]byte(row.EntityRefs), &got)
	if len(got) != 1 {
		t.Fatalf("二次回填后 entity_refs=%d, want 1", len(got))
	}
}

// 无强漏查的 shadow 条目不进反馈通道（MissingQueries 为空 → 跳过）。
func TestBackfillNoMissSkipped(t *testing.T) {
	newIsolatedDB(t)
	shadow := writeShadow(t, t.TempDir(), FeedbackShadowEntry{
		SessionID: "S2", Agent: "claude-code", Timestamp: "2026-08-27T10:00:00",
		EntityRefs: []ShadowEntityRef{{Type: "decision", ID: "decision-y", RefText: "约束"}},
	})
	sessions, refs, err := BackfillFeedback(shadow)
	if err != nil || sessions != 0 || refs != 0 {
		t.Fatalf("sessions=%d refs=%d err=%v, want 0/0（无强漏查跳过）", sessions, refs, err)
	}
	if row, _ := store.GetSessionSummary("S2"); row != nil {
		t.Fatalf("无强漏查不应写入 session_summaries: %+v", row)
	}
}

// L2 双前缀归一化：已有 L2 []string 形态（"task:task-x"）合并时拆成完整
// 反馈对象形态，写库端不产生 type:"" 脏对象（Claude Challenge 1）。
func TestBackfillNormalizeL2Prefix(t *testing.T) {
	newIsolatedDB(t)
	if err := store.UpsertSessionSummary(store.SessionSummary{
		SessionID: "S1", Source: "codex-cli",
		EntityRefs: `["task:task-20260615-abc","decision:decision-20260615-def"]`,
	}); err != nil {
		t.Fatal(err)
	}
	shadow := writeShadow(t, t.TempDir(), FeedbackShadowEntry{
		SessionID: "S1", Agent: "codex-cli", Timestamp: "2026-08-27T10:00:00",
		EntityRefs:     []ShadowEntityRef{{Type: "decision", ID: "decision-20260615-def", RefText: "约束"}},
		MissingQueries: []string{"aipm_get_decision"},
	})
	if _, _, err := BackfillFeedback(shadow); err != nil {
		t.Fatal(err)
	}
	row, _ := store.GetSessionSummary("S1")
	var got []FeedbackBackfillRef
	json.Unmarshal([]byte(row.EntityRefs), &got)
	if len(got) != 2 {
		t.Fatalf("entity_refs=%d, want 2（L2 两条 + 回填去重后 2 条）", len(got))
	}
	for _, r := range got {
		if r.Type == "" {
			t.Fatalf("存在 type 空脏对象: %+v", r)
		}
		if strings.HasPrefix(r.ID, "task:") || strings.HasPrefix(r.ID, "decision:") {
			t.Fatalf("id 未去双前缀: %+v", r)
		}
		if r.ID == "decision-20260615-def" && len(r.MissingQueries) == 0 {
			t.Fatalf("L2 引用补全后应带 missing_queries: %+v", r)
		}
	}
}

// 脏对象自我修复：已入库 type:"" 脏对象（旧 backfill 产物）在下次回填时
// 被规范化写回（changed 驱动），无需消费端兜底。
func TestBackfillSelfHealDirtyObject(t *testing.T) {
	newIsolatedDB(t)
	if err := store.UpsertSessionSummary(store.SessionSummary{
		SessionID: "S1", Source: "codex-cli",
		EntityRefs: `[{"type":"","id":"task:task-20260615-abc","ref_text":""}]`,
	}); err != nil {
		t.Fatal(err)
	}
	// 已存在的高价值 decision ref（added=0），但 task 脏对象需规范化——
	// changed 信号应驱动写回修复（重跑回填不重复计数）。
	shadow := writeShadow(t, t.TempDir(), FeedbackShadowEntry{
		SessionID: "S1", Agent: "codex-cli", Timestamp: "2026-08-27T10:00:00",
		EntityRefs:     []ShadowEntityRef{{Type: "decision", ID: "decision-x", RefText: "约束"}},
		MissingQueries: []string{"aipm_get_decision"},
	})
	sessions, refs, err := BackfillFeedback(shadow)
	if err != nil || sessions != 1 || refs != 1 {
		t.Fatalf("sessions=%d refs=%d err=%v, want 1/1（decision 新回填 + 脏对象修复）", sessions, refs, err)
	}
	row, _ := store.GetSessionSummary("S1")
	var got []FeedbackBackfillRef
	json.Unmarshal([]byte(row.EntityRefs), &got)
	if len(got) != 2 {
		t.Fatalf("entity_refs=%d, want 2（task 规范化 + decision）", len(got))
	}
	found := false
	for _, r := range got {
		if r.ID == "task-20260615-abc" {
			found = true
			if r.Type != "task" {
				t.Fatalf("task 脏对象未补 type: %+v", r)
			}
		}
		if r.Type == "" {
			t.Fatalf("写回后仍有 type 空脏对象: %+v", r)
		}
	}
	if !found {
		t.Fatalf("task 引用丢失: %+v", got)
	}
}

// 规范化冲突优先级：L2 空壳拆前缀后与既有 feedback 对象同 id——
// feedback（带 ref_text/missing_queries）必须胜出，不得被空壳踢掉
// （8/27 实测回归：019ffe3a feedback 对象在重跑中被规范化写坏）。
func TestBackfillNormKeepsFeedbackOnIDCollision(t *testing.T) {
	newIsolatedDB(t)
	// 混合形态：L2 拆前缀空壳（type 空、带双前缀）+ feedback 对象（同实体带 mq）
	if err := store.UpsertSessionSummary(store.SessionSummary{
		SessionID: "S1", Source: "codex-cli",
		EntityRefs: `[{"type":"","id":"decision:decision-x","ref_text":""},` +
			`{"type":"decision","id":"decision-x","ref_text":"约束 A","missing_queries":["aipm_get_decision"]}]`,
	}); err != nil {
		t.Fatal(err)
	}
	shadow := writeShadow(t, t.TempDir(), FeedbackShadowEntry{
		SessionID: "S1", Agent: "codex-cli", Timestamp: "2026-08-27T10:00:00",
		EntityRefs:     []ShadowEntityRef{{Type: "decision", ID: "decision-x", RefText: "约束 A"}},
		MissingQueries: []string{"aipm_get_decision"},
	})
	if _, _, err := BackfillFeedback(shadow); err != nil {
		t.Fatal(err)
	}
	row, _ := store.GetSessionSummary("S1")
	var got []FeedbackBackfillRef
	json.Unmarshal([]byte(row.EntityRefs), &got)
	if len(got) != 1 {
		t.Fatalf("entity_refs=%d, want 1（同 id 合并）", len(got))
	}
	r := got[0]
	if len(r.MissingQueries) == 0 || r.RefText != "约束 A" {
		t.Fatalf("feedback 对象被空壳踢掉/降级: %+v", r)
	}
}
