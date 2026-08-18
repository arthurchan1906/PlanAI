package eval

// EVAL_PIPELINE §3.4 阶段 3：意图段切分（episode）。
// 混合信号按强度排序：
//   任务型 user 消息 → 开新段（强制边界）
//   段内 commit 后 gap > CommitGapMinutes → 弱边界
//   相邻回合文件 Jaccard < 阈值 → 佐证（标记，不单独切分）
//   段内 commit → 段结束锚点（佐证）
// 阈值全部为校准参数（§3.5），标注集阶段用真实数据调整。

import (
	"fmt"
	"strings"
	"time"
)

// SegParams 切分参数（校准参数，默认值见 DefaultSegParams）。
type SegParams struct {
	CommitGapMinutes int     // 段内 commit 后 gap 超过该分钟数 → 弱边界（默认 60）
	JaccardThreshold float64 // 相邻回合文件集合 Jaccard 低于该值 → 佐证（默认 0.2）
}

// DefaultSegParams 返回规格 §3.4 默认参数。
func DefaultSegParams() SegParams {
	return SegParams{CommitGapMinutes: 60, JaccardThreshold: 0.2}
}

// CommitInfo 段内 commit 锚点（由调用方提供，如 git log 或 aipm_record_commit 记录）。
type CommitInfo struct {
	Hash      string
	CreatedAt time.Time
}

// Episode 意图段——评测的基本单位。
type Episode struct {
	ID         string
	SessionID  string
	Agent      string
	IntentText string   // 段首 user 消息（开段依据）
	Turns      []*Turn
	Files      []string // 段内全部回合引用文件（去重）
	Start      time.Time
	End        time.Time
	Commits    []string
	Boundary   string // forced_by_task / commit_gap / first
	JaccardHit bool   // 段内存在文件集合突变佐证（相邻回合 Jaccard < 阈值）
}

// SegmentEpisodes 按混合信号切分回合序列为意图段。
// classes[i] 对应 turns[i] 的意图分类；commits 按时间升序。
func SegmentEpisodes(sessionID, agent string, turns []Turn, classes []IntentClass, commits []CommitInfo, p SegParams) []Episode {
	if p.CommitGapMinutes <= 0 {
		p.CommitGapMinutes = 60
	}
	if p.JaccardThreshold <= 0 {
		p.JaccardThreshold = 0.2
	}

	var eps []Episode
	var cur *Episode
	lastCommit := time.Time{} // 当前段内最后 commit 时间
	commitIdx := 0            // commits 游标（每个 commit 只归属一个段）
	seq := 0
	prevFiles := []string{}

	for i := range turns {
		turn := &turns[i]
		boundary := ""
		switch {
		case classes[i].Type == IntentTask:
			boundary = "forced_by_task"
		case cur == nil:
			boundary = "first"
		case !lastCommit.IsZero() && turn.Start.Sub(lastCommit) > time.Duration(p.CommitGapMinutes)*time.Minute:
			boundary = "commit_gap"
		default:
			if len(prevFiles) > 0 && jaccard(prevFiles, turn.Files()) < p.JaccardThreshold {
				cur.JaccardHit = true
			}
		}
		if boundary != "" {
			seq++
			eps = append(eps, Episode{
				ID:         fmt.Sprintf("ep-%s-%03d", shortID(sessionID), seq),
				SessionID:  sessionID,
				Agent:      agent,
				IntentText: turn.UserMsg,
				Start:      turn.Start,
				End:        turn.End,
				Boundary:   boundary,
			})
			cur = &eps[len(eps)-1]
			lastCommit = time.Time{}
		}
		cur.Turns = append(cur.Turns, turn)
		if turn.Start.Before(cur.Start) {
			cur.Start = turn.Start
		}
		if turn.End.After(cur.End) {
			cur.End = turn.End
		}
		cur.Files = mergeFiles(cur.Files, turn.Files())
		prevFiles = turn.Files()
		// 归属段内 commit：created_at 不晚于当前回合结束，且不早于段开始
		for commitIdx < len(commits) && !commits[commitIdx].CreatedAt.After(turn.End) {
			cm := commits[commitIdx]
			if !cm.CreatedAt.Before(cur.Start) {
				cur.Commits = append(cur.Commits, cm.Hash)
				if lastCommit.IsZero() || cm.CreatedAt.After(lastCommit) {
					lastCommit = cm.CreatedAt
				}
			}
			commitIdx++
		}
	}
	return eps
}

// mergeFiles 去重合并文件集合。
func mergeFiles(a, b []string) []string {
	out := append([]string{}, a...)
	for _, f := range b {
		dup := false
		for _, o := range out {
			if o == f {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out
}

// jaccard 相邻文件集合相似度（Jaccard 系数）。
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1 // 双方皆空视为相似（无突变）
	}
	inA := map[string]bool{}
	for _, x := range a {
		inA[x] = true
	}
	inter, union := 0, len(a)
	for _, x := range b {
		if !inA[x] {
			union++
		} else {
			inter++
		}
	}
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

// shortID 取 session 前 8 字符作为 episode ID 的一部分。
func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return strings.ReplaceAll(s, "-", "")
}
