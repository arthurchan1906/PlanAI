package eval

// P0a2 主动触发（PROCESS_QUALITY_SPEC §2.1，v1.4 第二轮新增，第三轮补边界）。
// 定义：「该用未用 / 场景触发」——无信号进入时的主动性问题，与「信号-行为响应」并列。
// = 工具采用（该用的 aipm 工具用没用）：死循环时段应主动查历史但零 aipm 检索（c0ad2534
// 15h/16h 实证：全部 38 条 aipm 调用均发生在用户提示后，自发=0）；用户提示「查看 aipm/历史/
// 记录/跨 agent 讨论」后窗口内是否主动检索（c0ad2534 17:20 提示 → 30 秒内响应，正例）。
// 与「内容采纳」正交（一个是该用没用，一个是用了是否生效），不互相覆盖。
// P0a2 落点：方向性报告（每检测点出候选即成立），不承诺阈值/验收。

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ProactiveParams 主动触发参数。
type ProactiveParams struct {
	WindowMin int // 场景后主动检索窗口（默认 30 分钟）
}

// DefaultProactiveParams 默认参数。
func DefaultProactiveParams() ProactiveParams {
	return ProactiveParams{WindowMin: 30}
}

// ProactiveCandidate 主动触发候选（该用未用：场景事件后窗口内无自发 aipm 检索）。
type ProactiveCandidate struct {
	SceneAt       time.Time `json:"scene_at"`
	SceneKind     string    `json:"scene_kind"` // deadloop_no_aipm / deadloop_used_aipm / hint_responded / hint_missed
	SceneSnippet  string    `json:"scene_snippet,omitempty"`
	WindowMin     int       `json:"window_min"`
	SelfRetrieval int       `json:"self_retrieval"` // 场景后窗口内自发 aipm 检索数
	Note          string    `json:"note,omitempty"`
}

// recordHintRe 用户提示查记录/历史/aipm/跨 agent 讨论（工具采用场景触发）。
// 实证锚点：c0ad2534 17:20「每次在你修改代码之前 你或许最好可以查看搜索aipm中有没有相关记录」、
// 09:11「aipm中应该有提交记录」、10:09「查看一下aipm中的有关记录」；01a013f3「查看Claude的
// 讨论/分析/意见/建议」。首分支 = 查动词 + aipm/历史/记录；次分支 = aipm 附近出现记录/历史；
// 末分支 = 查看跨 agent 分析（read_discussions 例行工具的自然响应）。
var recordHintRe = regexp.MustCompile(`(?:查看|搜索|查查|查一下|查询|检查)[^。，;；]{0,12}(?:aipm|历史|记录)|aipm[^。，;；]{0,12}记录|历史记录|提交记录|查看[^。，]{0,30}(?:Claude|opencode|分析|讨论|意见|建议)`)

// aipmRetrievalInWindow 统计 [from, to] 内 aipm 工具调用数（mcp_aipm_* 全类 + 📡 text 行——
// 「工具采用」口径 = 用了 aipm 设施即可，不区分检索/状态读取/例行）。
// 按「工具名@秒」去重：mcp_tool + post_tool 双行 / 📡 text 行 = 同一次调用（P0b 实证）。
func aipmRetrievalInWindow(all []Record, from, to time.Time) int {
	if from.IsZero() || to.IsZero() {
		return 0
	}
	seen := map[string]bool{}
	n := 0
	for i := range all {
		r := &all[i]
		if r.CreatedAt.Before(from) || r.CreatedAt.After(to) {
			continue
		}
		name := aipmCallName(r)
		if name == "" {
			continue
		}
		key := aipmCallKey(name, r.CreatedAt)
		if seen[key] {
			continue
		}
		seen[key] = true
		n++
	}
	return n
}

// DetectProactiveTriggers 主动触发检测（工具采用）：
// 1) 死循环候选时段（T5 输出，非 Excluded）→ 时段内 aipm 检索数（0 = 该用未用候选）；
// 2) 用户提示「查看 aipm/历史/记录/跨 agent 讨论」→ 提示后窗口内是否主动检索（场景触发响应）。
func DetectProactiveTriggers(turns []Turn, deadloops []DeadloopCandidate, p ProactiveParams) []ProactiveCandidate {
	if p.WindowMin <= 0 {
		p.WindowMin = 30
	}
	var out []ProactiveCandidate
	// 全记录时间索引（跨 turn 扁平，与 T7 同法）
	var all []Record
	for i := range turns {
		for j := range turns[i].Records {
			all = append(all, turns[i].Records[j])
		}
	}
	// 1) 死循环时段：「该用未用」判定 = 时段内零自发 aipm 检索（SpontRetr，search/trace 非纠偏窗）。
	//    例行 read_discussions 不计——「零任意 aipm 调用」口径在 legacy 解析盲区下误报（P0b 实证
	//    c0ad2534 15h 3 条 📡 text 行 read，2026-08-24 修正为与定义「零自发」一致）。
	for i := range deadloops {
		if deadloops[i].Excluded {
			continue
		}
		d := &deadloops[i]
		if d.SpontRetr == 0 {
			out = append(out, ProactiveCandidate{
				SceneAt: d.Start, SceneKind: "deadloop_no_aipm",
				SceneSnippet: fmt.Sprintf("死循环候选 %s→%s（build=%d 自发检索=%d）", tsClock(d.Start), tsClock(d.End), d.Builds, d.SpontRetr),
				WindowMin:    p.WindowMin, SelfRetrieval: 0,
				Note: "该用未用：死循环时段零自发 aipm 检索（search/trace；例行 read_discussions 不计）",
			})
		} else {
			out = append(out, ProactiveCandidate{
				SceneAt: d.Start, SceneKind: "deadloop_used_aipm",
				SceneSnippet: fmt.Sprintf("死循环候选 %s→%s（build=%d 自发检索=%d）", tsClock(d.Start), tsClock(d.End), d.Builds, d.SpontRetr),
				WindowMin:    p.WindowMin, SelfRetrieval: d.SpontRetr,
			})
		}
	}
	// 2) 用户提示「查看 aipm/历史/记录」→ 提示后窗口内响应（场景触发）
	for i := range turns {
		t := &turns[i]
		if strings.TrimSpace(t.UserMsg) == "" || !recordHintRe.MatchString(t.UserMsg) {
			continue
		}
		used := aipmRetrievalInWindow(all, t.Start, t.Start.Add(time.Duration(p.WindowMin)*time.Minute))
		kind := "hint_missed"
		note := "该用未用：用户明确提示查记录/aipm/历史，窗口内未用 aipm 工具"
		if used > 0 {
			kind = "hint_responded"
			note = ""
		}
		out = append(out, ProactiveCandidate{
			SceneAt: t.Start, SceneKind: kind, SceneSnippet: snippetOf(t.UserMsg),
			WindowMin: p.WindowMin, SelfRetrieval: used, Note: note,
		})
	}
	return out
}
