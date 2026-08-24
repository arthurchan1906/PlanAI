package eval

// P0a1b T9 验收报告（PROCESS_QUALITY_SPEC §5 P0a 验收①-③ + EXECUTION_PLAN T9）。
// 聚合 T1-T5（process）+ T6（目标锚定）+ T7（构建产物）+ T8（五子信号），
// 比对验收①-③ 输出数据结果表（JSON + 人类可读双输出）。
//
// 验收①（事件边界切分）：正样本 = 死循环时段（6/23 15:00-17:00、6/24 09:00-09:11）；
//   召回 = 被 L1 标记正样本分钟数占比 ≥80%；误报 = 负样本被标记分钟数 ≤15。
// 验收②（方向性检查）：自发/被动比方向性 + 同项目对照（c0ad2534 vs 01a013f3 实测 48.6 倍）。
// 验收③（修正版冻结修订）：空壳构建单样本检出（01a013f3 10:25）+ 目标锚定负样本验证
//   （15:09 vs 4b41ba8 已知对准不被误报为错位；有效性未验证，不承诺阈值）。

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AcceptanceRow 验收①-③ 单行结果。
type AcceptanceRow struct {
	ID     string `json:"id"`     // 验收①/②/③
	Item   string `json:"item"`   // 子项（召回/误报/方向性/单样本…）
	Status string `json:"status"` // pass/fail/directional/not_run/record
	Detail string `json:"detail"`
}

// AcceptanceReport T9 验收报告。
type AcceptanceReport struct {
	SessionID   string           `json:"session_id"`
	Process     *ProcessReport   `json:"process"`              // T1-T5
	Anchor      *AnchorTarget    `json:"anchor,omitempty"`     // T6 输入（负样本验证）
	Anchoring   *AnchoringResult `json:"anchoring,omitempty"`  // T6 结果（anchor 传入时）
	Hollow      []HollowBuild    `json:"hollow_builds"`        // T7
	Signals     *SignalReport    `json:"signals,omitempty"`    // T8
	Acceptance  []AcceptanceRow  `json:"acceptance"`           // 验收①-③ 数据结果表
	Annotations []string         `json:"annotations,omitempty"`
	GeneratedAt time.Time        `json:"generated_at"`
}

// BuildAcceptanceReport T9 验收报告聚合。
// anchor 可选：验收③目标锚定负样本（子任务首条 user 消息 + 首个 commit 声称对象）。
// hollowSessionID 可选：验收③空壳构建正样本所在 session（01a013f3；零值 = 用主 session）。
func BuildAcceptanceReport(db *sql.DB, sessionID, fixHash, hollowSessionID string, anchor *AnchorTarget) (*AcceptanceReport, error) {
	rep := &AcceptanceReport{SessionID: sessionID, GeneratedAt: time.Now()}

	// T1-T5
	proc, err := BuildProcessReport(db, sessionID, fixHash)
	if err != nil {
		return nil, fmt.Errorf("T1-T5: %w", err)
	}
	rep.Process = proc

	// T8 五子信号（消费 T3 输出；commits = T2 fix commit）
	var commitTs []time.Time
	if proc.CommitLink != nil && !proc.CommitLink.CreatedAt.IsZero() {
		commitTs = append(commitTs, proc.CommitLink.CreatedAt)
	}
	turns, err := BuildTurns(db, sessionID)
	if err != nil {
		return nil, fmt.Errorf("T8 回合化: %w", err)
	}
	if len(proc.Feedback) > 0 || len(turns) > 0 {
		sig := BuildSignalReport(turns, proc.Feedback, commitTs, DefaultSignalParams())
		rep.Signals = &sig
	}

	// T6 目标锚定（负样本验证，存在性）
	if anchor != nil && anchor.UserMsg != "" && anchor.Claim != "" {
		rep.Anchor = anchor
		a := AnalyzeAnchoring(*anchor, DefaultAnchoringParams())
		rep.Anchoring = &a
	}

	// T7 构建产物（空壳构建单样本；正样本在 01a013f3 session）
	hollowSession := sessionID
	if hollowSessionID != "" {
		hollowSession = hollowSessionID
	}
	if hollowSession != "" {
		ht, err := BuildTurns(db, hollowSession)
		if err != nil {
			rep.Annotations = append(rep.Annotations, fmt.Sprintf("T7 空壳构建样本读取失败（%v）", err))
		} else if len(ht) > 0 {
			rep.Hollow = DetectHollowBuilds(ht, DefaultArtifactParams())
		}
	}

	// 验收①-③ 比对
	rep.Acceptance = acceptanceRows(rep)
	return rep, nil
}

// acceptanceWindows 验收①正/负样本时段（SPEC §5 P0a，事件边界切分）。
type acceptanceWindow struct {
	Start time.Time
	End   time.Time
}

var accept1Positive = []acceptanceWindow{
	{parseTs("2026-06-23T15:00:00"), parseTs("2026-06-23T17:00:00")}, // 死循环正样本（build 密集 35 次+零自发）
	{parseTs("2026-06-24T09:00:00"), parseTs("2026-06-24T09:11:00")}, // 活跃盲试段
}

var accept1Negative = []acceptanceWindow{
	{parseTs("2026-06-23T14:00:00"), parseTs("2026-06-23T15:00:00")}, // 正常时段
	{parseTs("2026-06-24T11:00:00"), parseTs("2026-06-24T11:04:00")}, // 纠偏响应
	{parseTs("2026-06-24T11:04:00"), parseTs("2026-06-24T11:37:00")}, // 修复执行
	{parseTs("2026-06-24T11:37:00"), parseTs("2026-06-24T11:48:00")}, // 确认+commit
	{parseTs("2026-06-24T11:48:00"), parseTs("2026-06-24T12:00:00")}, // 收尾
	{parseTs("2026-06-24T15:00:00"), parseTs("2026-06-24T16:00:00")}, // 15h 修复后
}

// acceptanceRows 验收①-③ 数据结果表。
func acceptanceRows(rep *AcceptanceReport) []AcceptanceRow {
	var rows []AcceptanceRow

	// ── 验收①：死循环时段分离（T5 候选 vs 事件边界）──
	positiveMin, negativeMin := 0, 0
	positiveTotal := windowMinutes(accept1Positive)
	if rep.Process != nil {
		for i := range rep.Process.Deadloops {
			c := &rep.Process.Deadloops[i]
			if c.Excluded {
				continue // 排除规则命中 = near-miss，不算 L1 标记
			}
			positiveMin += overlapMinutes(c.Start, c.End, accept1Positive)
			negativeMin += overlapMinutes(c.Start, c.End, accept1Negative)
		}
	}
	recall := 0.0
	if positiveTotal > 0 {
		recall = float64(positiveMin) / float64(positiveTotal)
	}
	recallOK := recall >= 0.8
	rows = append(rows, AcceptanceRow{
		ID: "验收①", Item: "死循环召回",
		Status: statusOf(recallOK, fmt.Sprintf("被标记正样本 %d/%d 分钟（%.0f%% ≥80%%）", positiveMin, positiveTotal, recall*100)),
	})
	rows = append(rows, AcceptanceRow{
		ID: "验收①", Item: "负样本误报",
		Status: statusOf(negativeMin <= 15, fmt.Sprintf("负样本被标记 %d 分钟（≤15）", negativeMin)),
	})

	// ── 验收②：方向性检查（自发/被动比）──
	if rep.Process != nil {
		r := rep.Process.Retrieval
		direction := "方向性记录"
		if r.Ratio > 0 {
			direction = fmt.Sprintf("自发/被动 = %d/%d（比 %.2f）", r.Spontaneous, r.Passive, r.Ratio)
		}
		rows = append(rows, AcceptanceRow{
			ID: "验收②", Item: "自发/被动比方向性",
			Status: "directional",
			Detail: direction + "；同项目对照 c0ad2534 vs 01a013f3 差异 ≥5 倍（规格实证 48.6 倍）",
		})
	}

	// ── 验收③：空壳构建单样本 + 目标锚定负样本 ──
	hollowFound := len(rep.Hollow) > 0
	rows = append(rows, AcceptanceRow{
		ID: "验收③", Item: "空壳构建单样本检出",
		Status: statusOf(hollowFound, fmt.Sprintf("空壳构建候选 %d 个（01a013f3 10:25 正样本应被规则命中）", len(rep.Hollow))),
	})
	if rep.Anchoring != nil {
		notMisreport := rep.Anchoring.Aligned || rep.Anchoring.Undecidable
		rows = append(rows, AcceptanceRow{
			ID: "验收③", Item: "目标锚定负样本验证",
			Status: statusOf(notMisreport, fmt.Sprintf("15:09 vs 4b41ba8 判定 %s（覆盖率 %.2f，不误报为错位）", alignedText(rep.Anchoring.Aligned), rep.Anchoring.Coverage)),
		})
	} else {
		rows = append(rows, AcceptanceRow{
			ID: "验收③", Item: "目标锚定负样本验证",
			Status: "not_run", Detail: "未传 anchor 参数（--anchor-msg/--claim）",
		})
	}
	return rows
}

func statusOf(ok bool, detail string) string {
	if ok {
		return "pass | " + detail
	}
	return "fail | " + detail
}

func alignedText(aligned bool) string {
	if aligned {
		return "对准"
	}
	return "目标错位候选"
}

// windowMinutes 窗口总分钟数。
func windowMinutes(ws []acceptanceWindow) int {
	total := 0
	for _, w := range ws {
		total += int(w.End.Sub(w.Start).Minutes())
	}
	return total
}

// overlapMinutes 候选时段与窗口集合的重叠分钟数。
func overlapMinutes(start, end time.Time, ws []acceptanceWindow) int {
	if start.IsZero() || end.IsZero() {
		return 0
	}
	total := 0
	for _, w := range ws {
		s, e := start, end
		if s.Before(w.Start) {
			s = w.Start
		}
		if e.After(w.End) {
			e = w.End
		}
		if e.After(s) {
			total += int(e.Sub(s).Minutes())
		}
	}
	return total
}

// FormatAcceptanceHuman T9 人类可读输出。
func FormatAcceptanceHuman(rep *AcceptanceReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "T9 验收报告（session %s，生成于 %s）\n", shortID(rep.SessionID), rep.GeneratedAt.Format("2006-01-02 15:04:05"))
	b.WriteString("\n── 验收①-③ 数据结果表 ──\n")
	for _, r := range rep.Acceptance {
		mark := "❌"
		switch {
		case strings.HasPrefix(r.Status, "pass"):
			mark = "✅"
		case strings.HasPrefix(r.Status, "fail"):
			mark = "❌"
		case strings.HasPrefix(r.Status, "directional"):
			mark = "➡️"
		case strings.HasPrefix(r.Status, "not_run"):
			mark = "⏸"
		}
		fmt.Fprintf(&b, "  %s %s｜%s：%s\n", mark, r.ID, r.Item, strings.TrimPrefix(r.Status, "pass | "))
	}
	if rep.Anchoring != nil {
		target := AnchorTarget{SessionID: rep.SessionID}
		if rep.Anchor != nil {
			target = *rep.Anchor
		}
		b.WriteString("\n" + FormatAnchoringHuman(*rep.Anchoring, target))
	}
	if len(rep.Hollow) > 0 {
		b.WriteString("\n" + FormatArtifactHuman(rep.Hollow))
	}
	if rep.Signals != nil && rep.Signals.Summary.Total > 0 {
		b.WriteString("\n" + FormatSignalHuman(*rep.Signals))
	}
	for _, a := range rep.Annotations {
		fmt.Fprintf(&b, "  ⚠ %s\n", a)
	}
	return b.String()
}


// parseTs 生产版时间解析（2006-01-02T15:04:05，失败返回零值）。
func parseTs(s string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05", s)
	return t
}
