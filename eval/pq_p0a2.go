package eval

// P0a2 方向性报告（EXECUTION_PLAN §1 P0a2 阶段产出 + §2.1 分层标注的 P0a2 层检测点）。
// 聚合：主动触发（工具采用）/ 静态可核对 / P3 计数基线（重复验证点、自建记录利用）。
// 落点 = 方向性报告（每检测点出候选即成立），不承诺阈值/验收（§2.1 口径冻结）。

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// P0a2Report P0a2 方向性报告（阶段 × 检测点 × 对照物映射）。
type P0a2Report struct {
	SessionID     string                          `json:"session_id"`
	GeneratedAt   time.Time                       `json:"generated_at"`
	Proactive     []ProactiveCandidate            `json:"proactive"`
	StaticChecks  []StaticCheckCandidate          `json:"static_checks"`
	RepeatedVerif []RepeatedVerificationCandidate `json:"repeated_verification"`
	SelfRecords   []SelfRecordCandidate           `json:"self_record_usage"`
	Annotations   []string                        `json:"annotations,omitempty"`
}

// BuildP0a2Report P0a2 方向性报告聚合。fixHashPrefix 复用 T2 关联（死循环候选 Cutoff 用）。
func BuildP0a2Report(db *sql.DB, sessionID, fixHashPrefix string) (*P0a2Report, error) {
	rep := &P0a2Report{SessionID: sessionID, GeneratedAt: time.Now()}

	// T1-T5 复用（回合 + 死循环候选 → 主动触发输入）
	proc, err := BuildProcessReport(db, sessionID, fixHashPrefix)
	if err != nil {
		return nil, fmt.Errorf("T1-T5: %w", err)
	}
	turns, err := BuildTurns(db, sessionID)
	if err != nil {
		return nil, fmt.Errorf("回合化: %w", err)
	}
	if len(turns) == 0 {
		rep.Annotations = append(rep.Annotations, "无回合数据（无 user 消息或全部被 isFakeUser 过滤）")
		return rep, nil
	}

	// P0a2-1 主动触发（工具采用）：死循环时段该用未用 + 用户提示后响应
	rep.Proactive = DetectProactiveTriggers(turns, proc.Deadloops, DefaultProactiveParams())
	// P0a2-2 静态可核对：真机轮次前 SDK 头文件核对
	rep.StaticChecks = DetectStaticCheckMisses(turns, DefaultStaticCheckParams())
	// P0a2-3 P3 计数基线：重复验证点 + 自建记录利用
	rep.RepeatedVerif = DetectRepeatedVerification(turns, DefaultRepeatedVerificationParams())
	rep.SelfRecords = DetectSelfRecordUsage(turns, DefaultSelfRecordParams())
	return rep, nil
}

// FormatP0a2Human P0a2 人类可读输出。
func FormatP0a2Human(rep *P0a2Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "P0a2 方向性报告（session %s，生成于 %s）\n", shortID(rep.SessionID), rep.GeneratedAt.Format("2006-01-02 15:04:05"))
	b.WriteString("\n── 主动触发（工具采用）──\n")
	if len(rep.Proactive) == 0 {
		b.WriteString("  无候选\n")
	}
	for _, c := range rep.Proactive {
		mark := "⚠"
		if c.SceneKind == "hint_responded" || c.SceneKind == "deadloop_used_aipm" {
			mark = "✓"
		}
		fmt.Fprintf(&b, "  %s %s %s [%s] aipm=%d %s\n", mark, tsFmt(c.SceneAt), c.SceneKind, snippetOf(c.SceneSnippet), c.SelfRetrieval, c.Note)
	}
	b.WriteString("\n── 静态可核对（真机轮次前 SDK 头文件核对）──\n")
	b.WriteString(FormatStaticCheckHuman(rep.StaticChecks))
	b.WriteString("\n── P3 计数基线：重复验证点 ──\n")
	b.WriteString(FormatRepeatedVerificationHuman(rep.RepeatedVerif))
	b.WriteString("\n── P3 计数基线：自建记录利用 ──\n")
	b.WriteString(FormatSelfRecordHuman(rep.SelfRecords))
	b.WriteString("\n── 检测点 × 对照物汇总（§9.2 对照物必查）──\n")
	for _, r := range P0a2DetectorSummary(rep) {
		fmt.Fprintf(&b, "  %s：候选 %d 个｜对照物 %s\n", r.Detector, r.Candidates, r.Anchor)
	}
	for _, a := range rep.Annotations {
		fmt.Fprintf(&b, "  ⚠ %s\n", a)
	}
	return b.String()
}

// P0a2DetectorRows 阶段 × 检测点 × 对照物映射（方向性报告汇总表，§9.2 对照物必查）。
type P0a2DetectorRow struct {
	Detector   string `json:"detector"`
	Definition string `json:"definition"`
	Anchor     string `json:"anchor"` // 实证对照物（真实数据）
	Candidates int    `json:"candidates"`
}

// P0a2DetectorSummary P0a2 检测点汇总表（配套方向性报告文档）。
func P0a2DetectorSummary(rep *P0a2Report) []P0a2DetectorRow {
	return []P0a2DetectorRow{
		{"主动触发·死循环时段该用未用", "死循环候选时段内零自发 aipm 检索（工具采用）", "c0ad2534 15h/16h（38 条 aipm 调用全在用户提示后）", countKind(rep.Proactive, "deadloop_no_aipm")},
		{"主动触发·用户提示后响应", "用户提示查记录/历史/aipm/跨 agent 讨论后窗口内 aipm 检索", "c0ad2534 17:20 提示 → 30 秒内响应", countKind(rep.Proactive, "hint_responded") + countKind(rep.Proactive, "hint_missed")},
		{"静态可核对", "真机轮次前窗口内无 SDK 头文件/API 签名核对", "01a013f3 10:52 等你真机验证 → 10:56 崩溃 → 11:15 才查头文件", len(rep.StaticChecks)},
		{"重复验证点", "同一验证点（无 fix commit/休眠间隔）重复真机验证请求", "01a013f3 8/19 晚 9 次 + 8/20 早 3 次请求 → 10:16 用户抗议", len(rep.RepeatedVerif)},
		{"自建记录利用", "记录创建后后续调试零 aipm 检索访问", "01a013f3 15:32 record_bug → 17:29 才首次检索", len(rep.SelfRecords)},
	}
}

func countKind(cands []ProactiveCandidate, kind string) int {
	n := 0
	for _, c := range cands {
		if c.SceneKind == kind {
			n++
		}
	}
	return n
}
