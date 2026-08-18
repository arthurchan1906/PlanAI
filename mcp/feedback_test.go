package mcp

import (
	"testing"
	"time"
)

func TestFeedbackTriageRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := MarkFeedbackProcessed("21"); err != nil {
		t.Fatalf("MarkFeedbackProcessed: %v", err)
	}
	feedbacks := []map[string]any{
		{"id": float64(21), "label": "suggestion", "content": "已处理项"},
		{"id": float64(22), "label": "suggestion", "content": "未处理项 A"},
		{"id": float64(23), "label": "bug", "content": "未处理项 B"},
	}
	unprocessed, inWindow := FeedbackTriageSnapshot(feedbacks, 30)
	if len(unprocessed) != 2 {
		t.Errorf("unprocessed = %d, want 2", len(unprocessed))
	}
	if inWindow != 1 {
		t.Errorf("inWindow = %d, want 1", inWindow)
	}
}

func TestFeedbackTriageRecheckWindowExpiry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// 40 天前处理过 → 已出 30 天复测窗口
	st := feedbackTriageState{Processed: map[string]string{
		"22": time.Now().AddDate(0, 0, -40).Format(time.RFC3339),
	}}
	if err := saveFeedbackTriage(st); err != nil {
		t.Fatalf("save: %v", err)
	}
	unprocessed, inWindow := FeedbackTriageSnapshot([]map[string]any{{"id": float64(22)}}, 30)
	if len(unprocessed) != 0 {
		t.Errorf("unprocessed = %d, want 0 (处理过的不算未处理)", len(unprocessed))
	}
	if inWindow != 0 {
		t.Errorf("inWindow = %d, want 0 (已出窗口)", inWindow)
	}
}

func TestFeedbackTriageReMarkResetsWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := feedbackTriageState{Processed: map[string]string{
		"23": time.Now().AddDate(0, 0, -40).Format(time.RFC3339),
	}}
	if err := saveFeedbackTriage(st); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := MarkFeedbackProcessed("23"); err != nil {
		t.Fatalf("MarkFeedbackProcessed: %v", err)
	}
	unprocessed, inWindow := FeedbackTriageSnapshot([]map[string]any{{"id": float64(23)}}, 30)
	if len(unprocessed) != 0 || inWindow != 1 {
		t.Errorf("after re-mark: unprocessed=%d inWindow=%d, want 0/1", len(unprocessed), inWindow)
	}
}

func TestFeedbackTriageInvalidIDString(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// feedback id 以 "25abc" 形式标记时不应崩溃，且不匹配数字 id
	if err := MarkFeedbackProcessed("25abc"); err != nil {
		t.Fatalf("MarkFeedbackProcessed: %v", err)
	}
	unprocessed, inWindow := FeedbackTriageSnapshot([]map[string]any{{"id": float64(25)}}, 30)
	if len(unprocessed) != 1 {
		t.Errorf("unprocessed = %d, want 1 (25abc 不应匹配 25)", len(unprocessed))
	}
	if inWindow != 0 {
		t.Errorf("inWindow = %d, want 0", inWindow)
	}
}
