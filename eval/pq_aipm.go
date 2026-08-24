package eval

// aipm 工具调用计数公共助手（P0b 候选确认发现，2026-08-24）：
// 同一次 aipm 调用在代理通道可能产生多行——mcp_tool 结果行 + post_tool 调用行 + 📡 摘要 text 行
// （实测 01a013f3 16:19:56 一次 aipm_list_sessions = 2 行，parse 后计 2 次）；且 legacy session
// 部分 aipm 调用仅以 📡 text 行存在（空 metadata，实测 c0ad2534 14:02:58 read_discussions）。
// 计数层统一：aipmCallName 归一化工具名（mcp 行 / 📡 text 行同语义），aipmCallKey 按「工具名@秒」
// 去重同一调用多行。parse.go 不动（避免影响 M1-M5 归因口径）。

import (
	"regexp"
	"strings"
	"time"
)

// aipmTextRowRe 📡 aipm_X 摘要行（legacy/空 meta 形态；✅ 为成功结果标记）。
var aipmTextRowRe = regexp.MustCompile(`📡\s*(aipm_[a-z_]+)`)

// aipmCallName 记录 → aipm 调用名（mcp_aipm_* 归一化；📡 text 行经 classifyMcp 同语义；非 aipm 返回空）。
func aipmCallName(r *Record) string {
	if strings.HasPrefix(r.Tool.Tool, "mcp_aipm_") {
		return r.Tool.Tool
	}
	if r.Role == "assistant" && strings.HasPrefix(r.Content, "📡") {
		if m := aipmTextRowRe.FindStringSubmatch(r.Content); m != nil {
			return classifyMcp(strings.ToLower(m[1]))
		}
	}
	return ""
}

// aipmCallKey 调用去重键（工具名@秒）：mcp_tool + post_tool 双行与 📡 text 行同秒去重。
func aipmCallKey(name string, ts time.Time) string {
	return name + "@" + ts.Format("20060102150405")
}
