package eval

// P0a1a 报告聚合（T1-T5 → ProcessReport，JSON + 人类可读双输出，复用 eval 双输出模式）。
// T9 验收报告在此基础上扩展（验收①-③ 数据结果表）。

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ProcessReport P0a1a 单 session 处理报告。
type ProcessReport struct {
	SessionID   string              `json:"session_id"`
	Boundary    *SessionBoundary    `json:"boundary"`
	CommitLink  *CommitLink         `json:"commit_link,omitempty"`
	Feedback    []FeedbackCandidate `json:"feedback"`
	Counts      FeedbackCounts      `json:"counts"`
	Retrieval   RetrievalStats      `json:"retrieval"`
	Deadloops   []DeadloopCandidate `json:"deadloops"`
	Annotations []string            `json:"annotations,omitempty"`
	GeneratedAt time.Time           `json:"generated_at"`
}

// BuildProcessReport T1-T5 聚合入口。
// fixHashPrefix 为 T2 关联核对的 commit hash 前缀（c0ad2534 样本 = d628b7a）。
func BuildProcessReport(db *sql.DB, sessionID, fixHashPrefix string) (*ProcessReport, error) {
	rep := &ProcessReport{SessionID: sessionID, GeneratedAt: time.Now()}

	b, err := BuildSessionBoundary(db, sessionID)
	if err != nil {
		return nil, fmt.Errorf("T1 时段边界: %w", err)
	}
	rep.Boundary = b

	if fixHashPrefix != "" {
		cl, err := LinkFixCommitByHash(db, fixHashPrefix)
		if err != nil {
			rep.Annotations = append(rep.Annotations, fmt.Sprintf("T2 关联核对失败（%v），End 未锚定", err))
		} else {
			rep.CommitLink = cl
			if !cl.CreatedAt.IsZero() {
				rep.Boundary.End = cl.CreatedAt
			}
			if cl.Weak {
				rep.Annotations = append(rep.Annotations, "T2 弱 ground truth：验收①降级为方向性检查")
			}
		}
	}

	turns, err := BuildTurns(db, sessionID)
	if err != nil {
		return nil, fmt.Errorf("回合化: %w", err)
	}
	if len(turns) == 0 {
		rep.Annotations = append(rep.Annotations, "无回合数据（无 user 消息或全部被 isFakeUser 过滤）")
	}

	// T3 反馈识别（modern = 019f 现代通道，ED 库 6/26 起，v1.3 七轮口径）
	modern := !b.Start.Before(modernChannelSince)
	rep.Feedback, rep.Counts = RecognizeFeedback(turns, modern)
	// T4 检索三分类
	rep.Retrieval = CountRetrieval(turns, rep.Feedback)
	// T5 死循环候选（自发/被动检索时间点 + commit 时间点；扫描上界 = fix commit 时刻，
	// 之后的新问题时段（如 11:50:08 KSN bug）不属于本问题域）
	var spontTs, passiveTs []time.Time
	for i := range turns {
		for j := range turns[i].Records {
			rec := turns[i].Records[j]
			if isHistoryRetrieval(rec.Tool) {
				if inCorrectionWindow(rec.CreatedAt, correctionTs(rep.Feedback)) {
					passiveTs = append(passiveTs, rec.CreatedAt)
				} else {
					spontTs = append(spontTs, rec.CreatedAt)
				}
			}
		}
	}
	var commitTs []time.Time
	var cutoff time.Time
	if rep.CommitLink != nil {
		commitTs = append(commitTs, rep.CommitLink.CreatedAt)
		cutoff = rep.CommitLink.CreatedAt
	}
	p := DefaultDeadloopParams()
	p.Cutoff = cutoff
	rep.Deadloops = FindDeadloops(turns, spontTs, passiveTs, commitTs, p)
	if strings.HasPrefix(sessionID, "c0ad2534") {
		// 对照物（§9.2 checkpoint）：死循环候选 ↔ §4.1 冻结小时表
		rep.Annotations = append(rep.Annotations, FrozenDeadloopAnnotations(rep.Deadloops)...)
	}
	return rep, nil
}

// modernChannelSince 019f 现代通道起点（ED 库 6/26 起两通道并存，v1.3 实证）。
var modernChannelSince = time.Date(2026, 6, 26, 0, 0, 0, 0, time.Local)

// FrozenDeadloopAnnotations T5 对照物：L1 死循环候选 ↔ c0ad2534 冻结小时表映射。
// 口径差异显式标注（§9.6 数据反馈驱动修订）。
// §10.11 归因：frozen build 列为行级 content grep 计数（非命令级语义分类），数值已作废——
// 对照物只保留判定方向（盲试/纠偏响应/修复期），不再展示作废 build 数值。
func FrozenDeadloopAnnotations(cands []DeadloopCandidate) []string {
	type row struct {
		h, frozen string
		pos       bool
	}
	rows := []row{
		{"2026-06-23T15", "正样本（盲试，判定=零自发+事件边界；frozen build 数值已作废 §10.11）", true},
		{"2026-06-23T16", "正样本（盲试，判定=零自发+事件边界；frozen build 数值已作废 §10.11）", true},
		{"2026-06-24T09", "正样本（09:00-09:11 活跃段；frozen build=17 为散文指令误计已作废 §10.11）", true},
		{"2026-06-24T10", "负样本（纠偏响应，被动=18 应排除；frozen build 数值已作废 §10.11）", false},
		{"2026-06-24T11", "负样本（修复验证期，commit 锚点应排除；frozen build 数值已作废 §10.11）", false},
	}
	var out []string
	for _, r := range rows {
		var hit *DeadloopCandidate
		for i := range cands {
			if cands[i].Start.Format("2006-01-02T15") == r.h {
				hit = &cands[i]
				break
			}
		}
		switch {
		case hit == nil && !r.pos:
			out = append(out, fmt.Sprintf("T5 对照物: %s %s — L1 未误报 ✓", r.h, r.frozen))
		case hit == nil:
			out = append(out, fmt.Sprintf("T5 对照物: %s %s — L1 未出候选（09h 数据差异已调查：09:00-09:11 零构建命令，frozen build=17 不可复现，§10.7）", r.h, r.frozen))
		case hit.Excluded:
			out = append(out, fmt.Sprintf("T5 对照物: %s %s — L1 near-miss（build=%d 自发=%d，%s）；frozen 判盲试 → 排除规则过严待校准", r.h, r.frozen, hit.Builds, hit.SpontRetr, hit.Reason))
		default:
			out = append(out, fmt.Sprintf("T5 对照物: %s %s — L1 候选命中 ✓", r.h, r.frozen))
		}
	}
	return out
}

func correctionTs(cands []FeedbackCandidate) []time.Time {
	var out []time.Time
	for _, c := range cands {
		if c.Kind == KindCorrection {
			out = append(out, c.Ts)
		}
	}
	return out
}

// FormatProcessHuman 人类可读输出（对齐 FormatHuman 风格）。
func FormatProcessHuman(rep *ProcessReport) string {
	var sb strings.Builder
	b := rep.Boundary
	fmt.Fprintf(&sb, "session %s\n", rep.SessionID)
	if b != nil {
		fmt.Fprintf(&sb, "  T1 时段边界: start=%s end=%s first_user=%s\n",
			tsFmt(b.Start), tsFmt(b.End), b.FirstUserID)
		for _, s := range b.SleepRanges {
			fmt.Fprintf(&sb, "    休眠: %s → %s（跨夜，不计停滞）\n", tsFmt(s.Start), tsFmt(s.End))
		}
	}
	if cl := rep.CommitLink; cl != nil {
		fmt.Fprintf(&sb, "  T2 修复 commit: %s %s (%s)\n", cl.Hash[:12], cl.Title, tsFmt(cl.CreatedAt))
		fmt.Fprintf(&sb, "    关联 bug: %s %s [fallback=%s weak=%v]\n", cl.BugID, cl.BugTitle, cl.Fallback, cl.Weak)
		for _, e := range cl.Evidence {
			fmt.Fprintf(&sb, "    证据: %s\n", e)
		}
	}
	fmt.Fprintf(&sb, "  T3 反馈识别: 介入=%d 纠偏=%d 推进=%d 存疑=%d 注入排除=%d（推进 = L1 关键词近似，含许可/疑问语义噪音，P1 L2 精化）\n",
		rep.Counts.Intervention, rep.Counts.Correction, rep.Counts.Progress,
		rep.Counts.Suspicious, rep.Counts.Injection)
	for _, c := range rep.Feedback {
		if c.Kind == KindCorrection || c.Kind == KindSuspicious {
			fmt.Fprintf(&sb, "    %s %s [%s] %s\n", tsFmt(c.Ts), c.UserMsgID, c.Kind, snippetOf(c.Snippet))
		}
	}
	fmt.Fprintf(&sb, "  T4 检索: 自发=%d 被动=%d 例行=%d 自发/被动=%s\n",
		rep.Retrieval.Spontaneous, rep.Retrieval.Passive, rep.Retrieval.Routine,
		ratioFmt(rep.Retrieval.Ratio))
	for _, d := range rep.Deadloops {
		fmt.Fprintf(&sb, "  T5 死循环候选: %s → %s build=%d fail=%d user=%d edit=%d 根因=%d 自发=%d 被动=%d%s\n",
			tsFmt(d.Start), tsFmt(d.End), d.Builds, d.Fails, d.UserMsgs, d.Edits, d.RootCause,
			d.SpontRetr, d.Passive,
			map[bool]string{true: "（" + d.Reason + "）", false: ""}[d.Excluded])
	}
	for _, a := range rep.Annotations {
		fmt.Fprintf(&sb, "  ! %s\n", a)
	}
	return sb.String()
}

func tsFmt(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func ratioFmt(r float64) string {
	if r == 0 {
		return "0（无被动检索）"
	}
	return fmt.Sprintf("%.2f", r)
}
