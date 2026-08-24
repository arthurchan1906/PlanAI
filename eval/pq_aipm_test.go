package eval

import (
	"testing"
)

// modern 双行（mcp_tool + post_tool）+ 📡 text 行（P0b 实证 01a013f3 16:19:56 形态）
func modernAipmRows(tool string, ts string) []Record {
	return []Record{
		{Role: "assistant", Content: "📡 " + tool + " ✅", Tool: ToolRecord{Tool: "mcp_aipm_list"}, CreatedAt: mustTs(ts)},
		{Role: "assistant", Content: "📡 " + tool, Tool: ToolRecord{Tool: "mcp_aipm_list"}, CreatedAt: mustTs(ts)},
		{Role: "assistant", Content: "📡 " + tool + " ✅ x", Tool: ToolRecord{Tool: "unknown"}, CreatedAt: mustTs(ts)},
	}
}

func TestAipmCallName(t *testing.T) {
	cases := []struct {
		name string
		rec  Record
		want string
	}{
		{"mcp 行", Record{Role: "assistant", Tool: ToolRecord{Tool: "mcp_aipm_search"}}, "mcp_aipm_search"},
		{"text 行 read", Record{Role: "assistant", Content: "📡 aipm_read_discussions ✅ last_n=10"}, "mcp_aipm_read"},
		{"text 行 search", Record{Role: "assistant", Content: "📡 aipm_search_context ✅ x"}, "mcp_aipm_search"},
		{"bash 非 aipm", Record{Role: "assistant", Content: "grep x", Tool: ToolRecord{Tool: "bash"}}, ""},
		{"assistant 普通文本", Record{Role: "assistant", Content: "分析结果：…"}, ""},
	}
	for _, c := range cases {
		if got := aipmCallName(&c.rec); got != c.want {
			t.Errorf("%s: aipmCallName = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAipmRetrievalInWindowDedup(t *testing.T) {
	// modern 一次 aipm_list_sessions 调用 = mcp_tool + post_tool 双行 + 📡 text 行 → 计 1
	rows := modernAipmRows("aipm_list_sessions", "2026-08-18T16:19:56")
	// 另加一次真实独立调用（不同秒）
	rows = append(rows, Record{Role: "assistant", Content: "📡 aipm_search_context ✅", Tool: ToolRecord{Tool: "mcp_aipm_search"}, CreatedAt: mustTs("2026-08-18T16:19:58")})
	n := aipmRetrievalInWindow(rows, mustTs("2026-08-18T16:19:00"), mustTs("2026-08-18T16:20:00"))
	if n != 2 {
		t.Errorf("窗口 aipm 调用 = %d, want 2（双行去重后 2 次独立调用）", n)
	}
}

func TestAipmRetrievalInWindowLegacyText(t *testing.T) {
	// legacy：仅 📡 text 行（空 meta）→ 计入（P0b [04] 实证 c0ad2534 14:02:58）
	rows := []Record{
		{Role: "assistant", Content: "📡 aipm_read_discussions ✅ last_n=10", Tool: ToolRecord{Tool: "unknown"}, CreatedAt: mustTs("2026-06-23T14:02:58")},
	}
	n := aipmRetrievalInWindow(rows, mustTs("2026-06-23T14:02:55"), mustTs("2026-06-23T14:03:30"))
	if n != 1 {
		t.Errorf("legacy text 行调用 = %d, want 1", n)
	}
}

func TestCountRetrievalDoubleRowDedup(t *testing.T) {
	turn := Turn{Records: []Record{
		{Role: "assistant", Tool: ToolRecord{Tool: "mcp_aipm_search"}, CreatedAt: mustTs("2026-06-24T09:10:00")},
		{Role: "assistant", Tool: ToolRecord{Tool: "mcp_aipm_search"}, CreatedAt: mustTs("2026-06-24T09:10:00")},                             // 双行
		{Role: "assistant", Content: "📡 aipm_search_context ✅", Tool: ToolRecord{Tool: "unknown"}, CreatedAt: mustTs("2026-06-24T09:10:00")}, // text 行
		{Role: "assistant", Tool: ToolRecord{Tool: "mcp_aipm_read"}, CreatedAt: mustTs("2026-06-24T09:11:00")},
		{Role: "assistant", Tool: ToolRecord{Tool: "mcp_aipm_read"}, CreatedAt: mustTs("2026-06-24T09:11:00")}, // 双行
	}}
	st := CountRetrieval([]Turn{turn}, nil)
	if st.Spontaneous != 1 || st.Routine != 1 {
		t.Errorf("retrieval = %+v, want 自发=1 例行=1（双行+text 行去重）", st)
	}
}

func TestIsAipmConsultTextRow(t *testing.T) {
	rec := Record{Role: "assistant", Content: "📡 aipm_search_discussions ✅ x", Tool: ToolRecord{Tool: "unknown"}}
	if !isAipmConsult(&rec) {
		t.Error("text 行 aipm_search_discussions 应识别为 consult")
	}
	rec2 := Record{Role: "assistant", Content: "📡 aipm_read_discussions ✅ last_n=10", Tool: ToolRecord{Tool: "unknown"}}
	if isAipmConsult(&rec2) {
		t.Error("read_discussions 不应是 consult（T4 边界同源）")
	}
}

func TestAipmCallKeySecondPrecision(t *testing.T) {
	a := mustTs("2026-08-18T16:19:56")
	b := mustTs("2026-08-18T16:19:56")
	if k1, k2 := aipmCallKey("mcp_aipm_list", a), aipmCallKey("mcp_aipm_list", b); k1 != k2 {
		t.Errorf("同秒同工具应同 key：%q vs %q", k1, k2)
	}
}
