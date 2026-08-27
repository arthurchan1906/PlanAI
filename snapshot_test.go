package main

import "testing"

// deltaMark 必须按数值比较而非字符串——"9.1%" 与 "42.4%" 字符串比较会误判
// （"9" > "4"），这是修复过的真实缺陷。
func TestSnapshotDeltaMarkNumericCompare(t *testing.T) {
	cases := []struct {
		before, after float64
		upGood        bool
		want          string
	}{
		{0.091, 0.424, true, "✅ 改善"},  // 9.1% → 42.4%，字符串比较会误判为恶化
		{0.424, 0.091, true, "❌ 恶化"},
		{7, 1, false, "✅ 改善"},         // action_items 越小越好
		{0.391566, 0.391566, true, "⚪ 未变"},
	}
	for _, c := range cases {
		if got := deltaMark(c.before, c.after, c.upGood); got != c.want {
			t.Errorf("deltaMark(%v,%v,upGood=%v) = %q, want %q", c.before, c.after, c.upGood, got, c.want)
		}
	}
}

func TestSnapshotP95(t *testing.T) {
	if got := p95([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}); got != 19 {
		t.Errorf("p95(1..20) = %v, want 19", got)
	}
	if got := p95(nil); got != 0 {
		t.Errorf("p95(nil) = %v, want 0", got)
	}
}

func TestParseSnapshotTime(t *testing.T) {
	if _, err := parseSnapshotTime("2026-08-27T00:00:00"); err != nil {
		t.Errorf("ISO 时间解析失败: %v", err)
	}
	if _, err := parseSnapshotTime("2026-08-27"); err != nil {
		t.Errorf("日期解析失败: %v", err)
	}
	if _, err := parseSnapshotTime("垃圾输入"); err == nil {
		t.Error("非法输入应报错")
	}
}

// consumptionTotalRate 空 map / 零 token 不应除零。
func TestSnapshotConsumptionTotalRateZero(t *testing.T) {
	if got := consumptionTotalRate(&snapshotDoc{}); got != 0 {
		t.Errorf("空快照 consumptionTotalRate = %v, want 0", got)
	}
}
