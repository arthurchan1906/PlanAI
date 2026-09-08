package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

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
	// 注: computeBehaviorBaseline 的 BlindTry 是「session 派生」口径(讨论行带 ❌/ERR 标记)。
	// CLI 报告路径会用权威 [MCP] 日志的 mcpErrRate 覆盖该值, 故此处断言覆盖的是
	// 纯 session 版分支, 不是最终报告值(最终报告值需集成测试验证)。
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

func TestPeerAwarenessBaseline(t *testing.T) {
	events := []ToolEvent{
		{SessionID: "sA", Agent: "codex", Tool: "aipm_list_sessions", Status: "ok"},
		{SessionID: "sA", Agent: "codex", Tool: "aipm_get_briefing", Status: "ok"},
		{SessionID: "sB", Agent: "claude", Tool: "aipm_search_context", Status: "ok"},
	}
	rep := computeBehaviorBaseline(events)
	if rep.TotalSessions != 2 || rep.TotalCalls != 3 {
		t.Fatalf("session/call count: %d/%d want 2/3", rep.TotalSessions, rep.TotalCalls)
	}
	if rep.PeerAwareness.Ratio != 0.5 || rep.PeerAwareness.Numerator != 1 || rep.PeerAwareness.Denominator != 2 {
		t.Fatalf("peer awareness: %+v want 1/2=0.5", rep.PeerAwareness)
	}
}

func TestMcpErrRateCrossArchive(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 主日志只从 9/2 起（与真实 aipmc.log 一致），8/29-9/1 在归档。
	write("aipmc.log", "[2026-09-02 14:30:53] [MCP] tool=aipm_get_commit status=OK\n[2026-09-03 09:00:00] [MCP] tool=aipm_record_bug status=ERR\n[2026-09-05 10:00:00] [MCP] tool=aipm_search_context status=OK\n")
	write("aipmc.log.20260902_140743", "[2026-08-29 16:00:00] [MCP] tool=aipm_read_discussions status=OK\n[2026-09-01 11:00:00] [MCP] tool=aipm_get_task status=ERR\n")
	// 窗口外归档段 + 非 aipm 工具行应被排除。
	write("aipmc.log.20260814_155529", "[2026-08-14 16:06:51] [MCP] tool=some_other status=ERR\n[2026-08-20 10:00:00] [MCP] tool=aipm_get_briefing status=OK\n")

	// 8/29-9/3: 归档 8/29(OK)、9/1(ERR) + 主日志 9/2(OK)、9/3(ERR) = 4 调用 / 2 报错。
	dim := mcpErrRate(dir, "2026-08-29", "2026-09-03")
	if dim.Denominator != 4 || dim.Numerator != 2 {
		t.Fatalf("window 8/29-9/3: %+v want denom 4 num 2", dim)
	}
	if math.Abs(dim.Ratio-0.5) > 1e-9 {
		t.Fatalf("window ratio = %v want 0.5", dim.Ratio)
	}

	// 全量(无界): 全部 aipm 调用 = 8/29 OK、9/1 ERR、8/20 OK、9/2 OK、9/3 ERR、9/5 OK = 6 / 2 报错。
	dim = mcpErrRate(dir, "", "")
	if dim.Denominator != 6 || dim.Numerator != 2 {
		t.Fatalf("full window: %+v want denom 6 num 2", dim)
	}
}
