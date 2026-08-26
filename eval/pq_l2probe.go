package eval

// P1c 对抗样本 + 稳定性探测（PROCESS_QUALITY_SPEC §2.3 约束③）。
// 把已知健康时段构造为死循环确认输入，跑 N 次 L2：
//   ① 对抗样本：健康时段期望 is_deadloop=false（不误判）——规格对照物 c0ad2534
//      06-23 14h（正常讨论+检索）/ 06-24 11h（修复验证期）
//   ② 稳定性：同候选跑 N 次，判定漂移率（一致率 = 多数判定占比）

import (
	"fmt"
	"strings"
	"time"
)

// VerdictRun 单次 L2 死循环确认判定。
type VerdictRun struct {
	IsDeadloop bool    `json:"is_deadloop"`
	Pattern    string  `json:"repeat_pattern,omitempty"`
	Confidence float64 `json:"confidence"`
	Error      string  `json:"error,omitempty"`
}

// ProbeResult 对抗样本探测结果（N 次判定 + 一致率）。
type ProbeResult struct {
	Label     string       `json:"label"`
	Start     time.Time    `json:"start"`
	End       time.Time    `json:"end"`
	Runs      int          `json:"runs"`
	Verdicts  []VerdictRun `json:"verdicts"`
	Agreement float64      `json:"agreement"` // 多数判定占比（1.0 = 全一致）
	Majority  string       `json:"majority"`  // 多数判定 "true"/"false"
}

// ProbeHealthyWindow 已知时段 → 死循环确认 N 次。
// confirmer nil → 降级（同 L2 编排：标注未运行不伪造）。
func ProbeHealthyWindow(confirmer L2Confirmer, turns []Turn, start, end time.Time, label string, runs int) (*ProbeResult, error) {
	if confirmer == nil {
		return &ProbeResult{Label: label, Start: start, End: end, Runs: runs, Majority: "未运行",
			Agreement: 0, Verdicts: []VerdictRun{{Error: "L2 未配置（LLM 确认器不可用）"}}}, nil
	}
	if runs <= 0 {
		runs = 3
	}
	// 已知健康时段构造为死循环候选输入（Reason 标注对抗样本，供 prompt 上下文区分）
	c := DeadloopCandidate{Start: start, End: end, Reason: "对抗样本（已知健康时段）: " + label}
	lines := l2CommandLines(l2RecordsBetween(turns, start, end), 0, 0)
	res := &ProbeResult{Label: label, Start: start, End: end, Runs: runs}
	trueN, okN := 0, 0
	for i := 0; i < runs; i++ {
		p := BuildDeadloopConfirmPrompt(c, lines)
		out, err := confirmer.Confirm(p)
		if err != nil {
			res.Verdicts = append(res.Verdicts, VerdictRun{Error: fmt.Sprintf("LLM: %v", err)})
			continue
		}
		parsed, err := ParseDeadloopConfirm(out)
		if err != nil {
			res.Verdicts = append(res.Verdicts, VerdictRun{Error: fmt.Sprintf("解析: %v", err)})
			continue
		}
		res.Verdicts = append(res.Verdicts, VerdictRun{
			IsDeadloop: parsed.IsDeadloop, Pattern: parsed.RepeatPattern, Confidence: parsed.Confidence,
		})
		if parsed.IsDeadloop {
			trueN++
		}
		okN++
	}
	if okN > 0 {
		if trueN*2 >= okN {
			res.Majority = "true"
			res.Agreement = float64(trueN) / float64(okN)
		} else {
			res.Majority = "false"
			res.Agreement = float64(okN-trueN) / float64(okN)
		}
	}
	return res, nil
}

// FormatProbeHuman 对抗样本探测人类可读输出。
func FormatProbeHuman(r *ProbeResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "对抗样本: %s（%s → %s）runs=%d\n", r.Label, tsFmt(r.Start), tsFmt(r.End), r.Runs)
	for i, v := range r.Verdicts {
		if v.Error != "" {
			fmt.Fprintf(&sb, "  run%d ✗ %s\n", i+1, v.Error)
			continue
		}
		fmt.Fprintf(&sb, "  run%d %s is_deadloop=%v conf=%.2f\n", i+1,
			map[bool]string{true: "⚠", false: "✓"}[v.IsDeadloop], v.IsDeadloop, v.Confidence)
	}
	fmt.Fprintf(&sb, "  多数判定=%s 一致率=%.0f%%（期望健康时段 is_deadloop=false）\n", r.Majority, r.Agreement*100)
	return sb.String()
}
