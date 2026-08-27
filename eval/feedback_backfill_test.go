package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
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
