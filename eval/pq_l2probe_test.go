package eval

// P1c 对抗样本 + 稳定性探测测试（§2.3 约束③）。

import (
	"strings"
	"testing"
)

// l2FlipConfirmer 按调用序号交替返回死循环/健康判定（模拟 LLM 漂移）。
type l2FlipConfirmer struct {
	n int
}

func (c *l2FlipConfirmer) Confirm(p L2Prompt) (string, error) {
	c.n++
	if c.n%2 == 1 {
		return `{"is_deadloop":false,"repeat_pattern":"","confidence":0.9}`, nil
	}
	return `{"is_deadloop":true,"repeat_pattern":"重复构建","confidence":0.8}`, nil
}

func TestProbeHealthyWindow(t *testing.T) {
	tr := pqTurn("u1", "查一下", "2026-06-23T14:00:00")
	tr.Records = append(tr.Records, pqRec("bash", "git log", "2026-06-23T14:10:00"))
	probe, err := ProbeHealthyWindow(&l2FlipConfirmer{}, []Turn{tr},
		ts("2026-06-23T14:00:00"), ts("2026-06-23T15:00:00"), "正常讨论+检索段", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Verdicts) != 3 {
		t.Fatalf("verdicts = %d, want 3", len(probe.Verdicts))
	}
	// 交替判定 → 一致率 2/3（多数 false，trueN=1）
	if probe.Majority != "false" || probe.Agreement < 0.66 || probe.Agreement > 0.67 {
		t.Errorf("Majority/Agreement = %s/%.2f, want false/~0.67", probe.Majority, probe.Agreement)
	}
	if !strings.Contains(FormatProbeHuman(probe), "一致率") {
		t.Errorf("人类可读输出缺一致率:\n%s", FormatProbeHuman(probe))
	}
}

func TestProbeHealthyWindowNilDegrade(t *testing.T) {
	probe, err := ProbeHealthyWindow(nil, nil, ts("2026-06-23T14:00:00"), ts("2026-06-23T15:00:00"), "x", 3)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Majority != "未运行" || len(probe.Verdicts) != 1 || probe.Verdicts[0].Error == "" {
		t.Errorf("nil confirmer 应降级标注未运行: %+v", probe)
	}
}

func TestProbeHealthyWindowAllHealthy(t *testing.T) {
	// 全健康判定 → 一致率 1.0、Majority=false（对抗样本期望：健康时段不误判）
	tr := pqTurn("u1", "查一下", "2026-06-24T11:00:00")
	probe, err := ProbeHealthyWindow(l2StubAllHealthy{}, []Turn{tr},
		ts("2026-06-24T11:00:00"), ts("2026-06-24T11:30:00"), "修复验证期", 3)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Majority != "false" || probe.Agreement != 1.0 {
		t.Errorf("全健康: Majority/Agreement = %s/%.2f, want false/1.0", probe.Majority, probe.Agreement)
	}
}

// l2StubAllHealthy 全返回健康判定的替身。
type l2StubAllHealthy struct{}

func (l2StubAllHealthy) Confirm(p L2Prompt) (string, error) {
	return `{"is_deadloop":false,"repeat_pattern":"","confidence":0.95}`, nil
}
