package eval

// P0a1a T4 历史检索意识（PROCESS_QUALITY_SPEC §2.1，口径冻结）。
// 三分类：自发（无纠偏窗口内）/ 被动（纠偏消息后 20 分钟内）/ 例行（read_discussions 等）。
// 定向边界：git show <旧commit>/log/blame/grep/rev-list + aipm_search_*/trace = 历史检索；
//   aipm_get_*/list_commits + git show HEAD:/status/diff = 状态读取（当前态），不计入；
//   aipm_read_discussions = 例行，不计入意识。

import (
	"strings"
	"time"
)

// RetrievalStats T4 检索三分类统计。
type RetrievalStats struct {
	Spontaneous int     `json:"spontaneous"` // 自发：非纠偏窗口内历史检索
	Passive     int     `json:"passive"`     // 被动：纠偏消息后 20 分钟内
	Routine     int     `json:"routine"`     // 例行：read_discussions
	Ratio       float64 `json:"ratio"`       // 自发/被动
}

// CorrectionWindow 纠偏后被动窗口（SPEC §2.1：20 分钟）。
const CorrectionWindow = 20 * time.Minute

// CountRetrieval 按回合序列统计检索三分类（turns 为 T3 RecognizeFeedback 的输入同源）。
func CountRetrieval(turns []Turn, candidates []FeedbackCandidate) RetrievalStats {
	// 纠偏时间窗口（被动窗口起点集合）
	var windows []time.Time
	for _, c := range candidates {
		if c.Kind == KindCorrection {
			windows = append(windows, c.Ts)
		}
	}
	var st RetrievalStats
	for i := range turns {
		for j := range turns[i].Records {
			rec := turns[i].Records[j]
			switch {
			case isRoutineRead(rec.Tool):
				st.Routine++
			case isHistoryRetrieval(rec.Tool):
				if inCorrectionWindow(rec.CreatedAt, windows) {
					st.Passive++
				} else {
					st.Spontaneous++
				}
			}
		}
	}
	if st.Passive > 0 {
		st.Ratio = float64(st.Spontaneous) / float64(st.Passive)
	}
	return st
}

// isHistoryRetrieval 定向边界：历史检索（SPEC §2.1 冻结）。
func isHistoryRetrieval(t ToolRecord) bool {
	switch t.Tool {
	case "mcp_aipm_search", "mcp_aipm_trace":
		return true
	case "bash":
		return gitHistoryCmd(t.Command)
	}
	return false
}

// isRoutineRead 例行：read_discussions（不计入检索意识）。
func isRoutineRead(t ToolRecord) bool {
	return t.Tool == "mcp_aipm_read"
}

// gitHistoryCmd git 历史检索命令（git show <旧commit>/log/blame/grep/rev-list）。
// git show HEAD:（当前态）不算；git status/diff（工作区 vs HEAD 当前态）不算。
func gitHistoryCmd(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	for _, k := range []string{"git log", "git blame", "git grep", "git rev-list"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	if strings.Contains(lower, "git show") {
		return !gitShowCurrentState(lower)
	}
	return false
}

// gitShowCurrentState git show HEAD 当前态判定（Claude 审核 8/24）：
// HEAD / HEAD:path 为当前态（不计历史检索）；HEAD~1 / HEAD^ / HEAD@{n}
// 是旧 commit 引用，应算历史检索——原实现 substring「git show head」把
// HEAD~1/HEAD^ 也误排除（HEAD~1 含前缀 "head"）。
func gitShowCurrentState(lower string) bool {
	i := strings.Index(lower, "git show")
	ref := strings.TrimSpace(lower[i+len("git show"):])
	if sp := strings.IndexAny(ref, " \t\n"); sp >= 0 {
		ref = ref[:sp]
	}
	ref = strings.ToLower(ref)
	if !strings.HasPrefix(ref, "head") {
		return false // 非 HEAD 引用（git show <hash>）= 历史 commit
	}
	rest := ref[len("head"):]
	// 当前态：HEAD（空引用）或 HEAD:path；含 ~ ^ @ 的祖先引用 = 历史
	return rest == "" || strings.HasPrefix(rest, ":") || !strings.ContainsAny(rest, "~^@")
}

func inCorrectionWindow(ts time.Time, windows []time.Time) bool {
	for _, w := range windows {
		if !ts.Before(w) && ts.Sub(w) <= CorrectionWindow {
			return true
		}
	}
	return false
}
