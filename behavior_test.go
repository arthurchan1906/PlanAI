package main

import "testing"

func TestParseToolRow(t *testing.T) {
	ev, ok := parseToolRow("s1", "codex-cli", "📡 aipm_get_briefing ✅", "2026-09-08T10:00:00")
	if !ok || ev.Tool != "aipm_get_briefing" || ev.Status != "ok" || ev.SessionID != "s1" {
		t.Fatalf("parse ok row: ok=%v ev=%+v", ok, ev)
	}
	ev, ok = parseToolRow("s2", "claude-code", "📡 aipm_record_bug ❌ sql: expected 14", "2026-09-08T11:00:00")
	if !ok || ev.Tool != "aipm_record_bug" || ev.Status != "err" {
		t.Fatalf("parse err row: ok=%v ev=%+v", ok, ev)
	}
	ev, ok = parseToolRow("s5", "claude-code", "🛠 mcp__aipm__aipm_read_discussions", "2026-09-08T12:00:00")
	if !ok || ev.Tool != "aipm_read_discussions" || ev.Status != "ok" {
		t.Fatalf("parse mcp__aipm row: ok=%v ev=%+v", ok, ev)
	}
	if _, ok := parseToolRow("s3", "codex-cli", "some plain text", ""); ok {
		t.Fatalf("plain text should be unparseable")
	}
	if _, ok := parseToolRow("s4", "codex-cli", "📡", ""); ok {
		t.Fatalf("bare emoji+pitch should be unparseable")
	}
}

func TestComputeBehaviorBaseline(t *testing.T) {
	events := []ToolEvent{
		{SessionID: "sA", Agent: "codex", Tool: "aipm_get_briefing", Status: "ok"},
		{SessionID: "sA", Agent: "codex", Tool: "aipm_create_plan", Status: "ok"},
		{SessionID: "sA", Agent: "codex", Tool: "aipm_search_context", Status: "ok"},
		{SessionID: "sB", Agent: "claude", Tool: "aipm_record_bug", Status: "err"},
		{SessionID: "sB", Agent: "claude", Tool: "aipm_read_discussions", Status: "ok"},
	}
	rep := computeBehaviorBaseline(events)
	if rep.TotalSessions != 2 || rep.TotalCalls != 5 {
		t.Fatalf("session/call count: %d/%d want 2/5", rep.TotalSessions, rep.TotalCalls)
	}
	if rep.RetrievalAwareness.Ratio != 0.6 || rep.RetrievalAwareness.Numerator != 3 || rep.RetrievalAwareness.Denominator != 5 {
		t.Fatalf("retrieval awareness: %+v want 3/5=0.6", rep.RetrievalAwareness)
	}
	if rep.Planfulness.Ratio != 0.5 || rep.Planfulness.Numerator != 1 || rep.Planfulness.Denominator != 2 {
		t.Fatalf("planfulness: %+v want 1/2=0.5", rep.Planfulness)
	}
	if rep.BlindTry.Ratio != 0.2 || rep.BlindTry.Numerator != 1 || rep.BlindTry.Denominator != 5 {
		t.Fatalf("blind try: %+v want 1/5=0.2", rep.BlindTry)
	}
	if len(rep.TopTools) != 5 {
		t.Fatalf("top tools len = %d want 5: %+v", len(rep.TopTools), rep.TopTools)
	}
	foundBrief, foundBugErr := false, false
	for _, tt := range rep.TopTools {
		if tt.Tool == "aipm_get_briefing" && tt.Calls == 1 {
			foundBrief = true
		}
		if tt.Tool == "aipm_record_bug" && tt.Errors == 1 {
			foundBugErr = true
		}
	}
	if !foundBrief || !foundBugErr {
		t.Fatalf("top tools missing expected entries: %+v", rep.TopTools)
	}
}
