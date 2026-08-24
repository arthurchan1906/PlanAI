package eval

// P0a1b T8 命令级五子信号（PROCESS_QUALITY_SPEC §2.1 反馈响应性，命令级）。
// 响应（纠偏后 20min 内新行为）/ 持续近似（旧对象不重现）/ 收敛（用户确认或修复 commit，
// 不绑 agent 自报）/ 对准近似（响应对象 vs 反馈所指——所指实体由 T3 提取输出，T8 消费）。
// 加深（对象扩展率）为对象级、P0a1 不承诺；对准近似 = 报告输出（方向性），不占验收①-③（v1.3）。
// 判定均为 L1 近似候选：✗/存疑 组合交 L2 确认（v1.4 第四轮 L1/L2 分工）。

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// SignalParams T8 五子信号参数。
type SignalParams struct {
	ResponseWindowMin int // 纠偏后响应窗口（默认 20 分钟，规格）
	PersistWindowMin  int // 持续近似窗口：响应后旧命令不重现（默认 60 分钟）
}

// DefaultSignalParams 默认参数。
func DefaultSignalParams() SignalParams {
	return SignalParams{ResponseWindowMin: 20, PersistWindowMin: 60}
}

// SignalRow 一个反馈事件 → 命令级五子信号判定行。
type SignalRow struct {
	FeedbackTs  time.Time    `json:"feedback_ts"`
	Kind        FeedbackKind `json:"kind"`
	Snippet     string       `json:"snippet"`
	Referents   []string     `json:"referents"` // 反馈所指（T3 提取输出）
	Response    bool         `json:"response"`
	ResponseAt  time.Time    `json:"response_at,omitempty"`
	ResponseObj string       `json:"response_obj,omitempty"` // 响应行为（edit 文件/命令）
	Persistent  bool         `json:"persistent"`             // 持续近似：响应后旧命令不重现
	Converged   bool         `json:"converged"`              // 收敛：用户确认或修复 commit（不绑自报）
	Aligned     bool         `json:"aligned"`                // 对准近似：响应对象 vs 反馈所指
	Evidence    []string     `json:"evidence"`
}

// SignalSummary T8 汇总。
type SignalSummary struct {
	Total      int `json:"total"`       // 反馈事件总数（纠偏+介入+推进）
	Responsive int `json:"responsive"`  // 响应 ✓
	Persistent int `json:"persistent"`  // 持续近似 ✓
	Converged  int `json:"converged"`   // 收敛 ✓
	Aligned    int `json:"aligned"`     // 对准近似 ✓
	NoResponse int `json:"no_response"` // 无响应行为（响应 ✗）
}

// SignalReport T8 五子信号报告。
type SignalReport struct {
	Rows    []SignalRow   `json:"rows"`
	Summary SignalSummary `json:"summary"`
}

// BuildSignalReport T8 命令级五子信号判定。
// feedback = T3 输出（含 Referents 反馈所指）；commits = 修复 commit 时间点（T2 输出）。
func BuildSignalReport(turns []Turn, feedback []FeedbackCandidate, commits []time.Time, p SignalParams) SignalReport {
	if p.ResponseWindowMin <= 0 {
		p.ResponseWindowMin = 20
	}
	if p.PersistWindowMin <= 0 {
		p.PersistWindowMin = 60
	}
	rep := SignalReport{}
	// 时间索引：feedback Ts → 后续记录流（扁平化）
	var allRecs []Record
	recOf := map[int]int{} // 记录下标 → turn 下标
	for i := range turns {
		for j := range turns[i].Records {
			recOf[len(allRecs)] = i
			allRecs = append(allRecs, turns[i].Records[j])
		}
	}
	userMsgs := userMessagesAfter(turns, allRecs)
	for _, fb := range feedback {
		if fb.Kind != KindCorrection && fb.Kind != KindManual && fb.Kind != KindProgress {
			continue // 存疑/注入不参与五子信号
		}
		row := SignalRow{
			FeedbackTs: fb.Ts, Kind: fb.Kind, Snippet: fb.Snippet, Referents: fb.Referents,
		}
		// 响应：纠偏后窗口内第一个新行为（edit/write 或非检索新命令）
		prevCmds := commandsInWindow(allRecs, fb.Ts.Add(-time.Duration(p.ResponseWindowMin)*time.Minute), fb.Ts)
		firstNew := findFirstNewBehavior(allRecs, fb.Ts, prevCmds, p.ResponseWindowMin)
		if firstNew != nil {
			row.Response = true
			row.ResponseAt = firstNew.CreatedAt
			row.ResponseObj = responseObject(firstNew)
			row.Evidence = append(row.Evidence,
				fmt.Sprintf("响应：%s 后 %s 出现新行为（%s）", fb.Ts.Format("15:04:05"), firstNew.CreatedAt.Sub(fb.Ts).Round(time.Minute), snippetOf(firstNew.Content)))
			// 持续近似：响应后窗口内无同命令重复
			row.Persistent = !sameCmdRepeats(allRecs, firstNew, p.PersistWindowMin)
			if row.Persistent {
				row.Evidence = append(row.Evidence, "持续近似：响应后旧命令未重现")
			} else {
				row.Evidence = append(row.Evidence, "持续近似 ✗：响应后同命令重现")
			}
			// 对准近似：响应对象 vs 反馈所指
			row.Aligned = alignedWithReferents(row.ResponseObj, row.Referents)
			if row.Aligned {
				row.Evidence = append(row.Evidence, "对准近似：响应对象与反馈所指共享主题")
			} else {
				row.Evidence = append(row.Evidence, "对准近似 ✗：响应对象与反馈所指无共享（交 L2）")
			}
		} else {
			row.Evidence = append(row.Evidence, fmt.Sprintf("响应 ✗：%s 后 %d 分钟内无新行为（交 L2）", fb.Ts.Format("15:04:05"), p.ResponseWindowMin))
		}
		// 收敛：用户确认词或修复 commit（不绑 agent 自报）
		row.Converged = userConfirmed(userMsgs, fb.Ts) || commitAfter(commits, fb.Ts)
		if row.Converged {
			row.Evidence = append(row.Evidence, "收敛：用户确认或修复 commit 出现")
		}
		rep.Rows = append(rep.Rows, row)
	}
	// 汇总
	rep.Summary.Total = len(rep.Rows)
	for i := range rep.Rows {
		if rep.Rows[i].Response {
			rep.Summary.Responsive++
		} else {
			rep.Summary.NoResponse++
		}
		if rep.Rows[i].Persistent {
			rep.Summary.Persistent++
		}
		if rep.Rows[i].Converged {
			rep.Summary.Converged++
		}
		if rep.Rows[i].Aligned {
			rep.Summary.Aligned++
		}
	}
	return rep
}

// userMessagesAfter 反馈 Ts 之后的 user 消息列表（含时间）。
func userMessagesAfter(turns []Turn, recs []Record) []Turn {
	// 简化：返回全部 user 回合（带 Ts），调用方按时间过滤
	return turns
}

// commandsInWindow 收集 [from, to) 内的 bash 命令。
func commandsInWindow(recs []Record, from, to time.Time) map[string]bool {
	out := map[string]bool{}
	for i := range recs {
		r := &recs[i]
		if r.Tool.Tool == "bash" && !r.CreatedAt.Before(from) && r.CreatedAt.Before(to) {
			out[r.Tool.Command] = true
		}
	}
	return out
}

// findFirstNewBehavior 找 after 之后窗口内第一个新行为。
// 新行为 = edit/write 记录，或非检索 bash 命令且与 prevCmds 不同。
func findFirstNewBehavior(recs []Record, after time.Time, prevCmds map[string]bool, windowMin int) *Record {
	for i := range recs {
		r := &recs[i]
		if !r.CreatedAt.After(after) {
			continue
		}
		if r.CreatedAt.Sub(after) > time.Duration(windowMin)*time.Minute {
			return nil
		}
		switch {
		case r.Tool.Tool == "edit" || r.Tool.Tool == "write":
			return r
		case r.Tool.Tool == "bash" && !isHistoryRetrieval(r.Tool):
			if !prevCmds[r.Tool.Command] {
				return r
			}
		}
	}
	return nil
}

// responseObject 响应行为摘要：edit/write → 文件名；bash → 命令（短）。
func responseObject(r *Record) string {
	if (r.Tool.Tool == "edit" || r.Tool.Tool == "write") && len(r.Tool.Files) > 0 {
		return filepath.Base(r.Tool.Files[0])
	}
	if r.Tool.Tool == "bash" {
		return snippetOf(r.Tool.Command)
	}
	return r.Tool.Command
}

// sameCmdRepeats 响应后窗口内同一命令是否重复（持续近似 ✗ 信号）。
func sameCmdRepeats(recs []Record, resp *Record, windowMin int) bool {
	for i := range recs {
		r := &recs[i]
		if !r.CreatedAt.After(resp.CreatedAt) {
			continue
		}
		if r.CreatedAt.Sub(resp.CreatedAt) > time.Duration(windowMin)*time.Minute {
			return false
		}
		if r.Tool.Tool == resp.Tool.Tool && r.Tool.Command != "" && r.Tool.Command == resp.Tool.Command {
			return true
		}
	}
	return false
}

// alignedWithReferents 对准近似：响应对象主题词与反馈所指共享核心词。
// L1 候选（低精度高召回）：CJK 主题词 + 英文 token 经领域映射（Share→分享）后匹配；
// 精确语义判定归 L2 matched_object（P1 接入）。
func alignedWithReferents(responseObj string, referents []string) bool {
	objWords := extractObjWords(responseObj)
	for _, o := range objWords {
		for _, ref := range referents {
			if strings.Contains(o, ref) || strings.Contains(ref, o) {
				return true
			}
		}
	}
	return false
}

// extractObjWords 响应对象主题词：CJK 主题词 + 英文驼峰 token 领域映射。
func extractObjWords(obj string) []string {
	words := extractReferents(obj)
	lower := strings.ToLower(obj)
	for en, zh := range l1EnZhMap {
		if strings.Contains(lower, en) {
			words = append(words, zh)
		}
	}
	return words
}

// l1EnZhMap L1 领域映射（对准近似近似匹配用；精确语义归 L2）。
var l1EnZhMap = map[string]string{
	"share": "分享", "log": "日志", "import": "导入", "url": "跳转",
	"queue": "队列", "vault": "保险库", "file": "文件", "image": "图片",
	"photo": "图片", "jump": "跳转", "transfer": "直传", "scene": "场景",
	"extension": "扩展", "open": "打开", "inbox": "导入队列", "dzsec": "资云集",
}

// userConfirmed 反馈后是否存在用户确认词（收敛信号，不绑 agent 自报）。
func userConfirmed(turns []Turn, after time.Time) bool {
	for i := range turns {
		t := &turns[i]
		if !t.Start.After(after) {
			continue
		}
		if strings.TrimSpace(t.UserMsg) == "" {
			continue
		}
		lower := strings.ToLower(t.UserMsg)
		for _, k := range []string{"好的", "可以了", "成功了", "搞定了", "解决了", "没问题", "完美", "正常了", "修好了", "ok", "nice", "great"} {
			if strings.Contains(lower, k) {
				return true
			}
		}
	}
	return false
}

// commitAfter 反馈后是否存在 commit。
func commitAfter(commits []time.Time, after time.Time) bool {
	for _, c := range commits {
		if c.After(after) {
			return true
		}
	}
	return false
}

// FormatSignalHuman T8 人类可读输出（判定表）。
func FormatSignalHuman(rep SignalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "T8 命令级五子信号（反馈事件 %d 个：响应 %d / 持续 %d / 收敛 %d / 对准 %d；L1 近似，✗/存疑交 L2）\n",
		rep.Summary.Total, rep.Summary.Responsive, rep.Summary.Persistent, rep.Summary.Converged, rep.Summary.Aligned)
	limit := len(rep.Rows)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		r := &rep.Rows[i]
		fmt.Fprintf(&b, "  [%d] %s %s「%s」\n", i+1, r.FeedbackTs.Format("01-02 15:04"), r.Kind, r.Snippet)
		mark := func(v bool) string {
			if v {
				return "✓"
			}
			return "✗"
		}
		fmt.Fprintf(&b, "      响应 %s | 持续近似 %s | 收敛 %s | 对准近似 %s\n",
			mark(r.Response), mark(r.Persistent), mark(r.Converged), mark(r.Aligned))
		if len(r.Evidence) > 0 {
			fmt.Fprintf(&b, "      证据：%s\n", strings.Join(r.Evidence, "；"))
		}
	}
	if len(rep.Rows) > limit {
		fmt.Fprintf(&b, "  …（余 %d 行，完整判定表见 JSON）\n", len(rep.Rows)-limit)
	}
	return b.String()
}
