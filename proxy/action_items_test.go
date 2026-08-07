package proxy

import (
	"strings"
	"testing"
)

func TestFormatActionItemsPriorityAndAggregation(t *testing.T) {
	list := []emergeEvent{
		{typ: "tentative_link", entityID: "x", summary: "link", createdAt: "2026-08-07T10:00:00"},
		{typ: "hotspot_untracked", entityID: "/repo/a.go", summary: "file a", createdAt: "2026-08-07T10:00:01"},
		{typ: "hotspot_untracked", entityID: "/repo/b.go", summary: "file b", createdAt: "2026-08-07T10:00:02"},
		{typ: "mcp_error", entityID: "tool1", summary: "工具 t 失败", createdAt: "2026-08-07T10:00:03"},
		{typ: "commit_orphan", entityID: "c1", summary: "orphan 1", createdAt: "2026-08-07T10:00:04"},
		{typ: "commit_orphan", entityID: "c2", summary: "orphan 2", createdAt: "2026-08-07T10:00:05"},
		{typ: "task_stale_file", entityID: "t1", summary: "stale 1", createdAt: "2026-08-07T10:00:06"},
	}
	// prio fields are set by eventPriority in production; emulate here.
	for i := range list {
		list[i].prio = eventPriority(list[i].typ)
	}
	items := formatActionItems(list)

	if len(items) != 6 {
		t.Fatalf("want 6 items (2 aggregated + 4 individual), got %d: %v", len(items), items)
	}
	// hotspot + mcp_error each aggregate to one line
	var hotspot, mcpErr, orphanCount int
	for _, it := range items {
		switch {
		case strings.Contains(it, "个文件被多 session 修改"):
			hotspot++
			if !strings.Contains(it, "b.go") || !strings.Contains(it, "2 个文件") {
				t.Errorf("hotspot line should mention both files and count 2: %s", it)
			}
		case strings.Contains(it, "个 MCP 工具调用失败"):
			mcpErr++
		case strings.Contains(it, "orphan"):
			orphanCount++
		}
	}
	if hotspot != 1 {
		t.Errorf("want 1 aggregated hotspot line, got %d", hotspot)
	}
	if mcpErr != 1 {
		t.Errorf("want 1 aggregated mcp_error line, got %d", mcpErr)
	}
	if orphanCount != 2 {
		t.Errorf("want 2 individual orphan lines, got %d", orphanCount)
	}
	// Priority: the low-signal tentative_link (prio 0) must come last
	if !strings.Contains(items[len(items)-1], "link") {
		t.Errorf("lowest-priority item should be last, got: %s", items[len(items)-1])
	}
}

func TestFormatActionItemsPerTypeCap(t *testing.T) {
	var list []emergeEvent
	for i := 0; i < 12; i++ {
		list = append(list, emergeEvent{
			typ: "commit_orphan", entityID: "c", summary: "orphan", createdAt: "2026-08-07T10:00:00",
			prio: eventPriority("commit_orphan"),
		})
	}
	items := formatActionItems(list)
	if len(items) != perTypeCap {
		t.Errorf("per-type cap should limit orphans to %d, got %d", perTypeCap, len(items))
	}
}
